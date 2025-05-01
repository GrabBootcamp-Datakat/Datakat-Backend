package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/fx"

	"skeleton-internship-backend/config"
	"skeleton-internship-backend/database"
	_ "skeleton-internship-backend/docs" // This will be created by swag
	"skeleton-internship-backend/internal/controller"
	"skeleton-internship-backend/internal/elasticsearch"
	"skeleton-internship-backend/internal/filestate"
	"skeleton-internship-backend/internal/kafka"
	"skeleton-internship-backend/internal/metrics"
	"skeleton-internship-backend/internal/parser"
	"skeleton-internship-backend/internal/repository"
	"skeleton-internship-backend/internal/scheduler"
	"skeleton-internship-backend/internal/service"
	"skeleton-internship-backend/internal/timescaledb"
)

// @title           Todo List API
// @version         1.0
// @description     A modern RESTful API for managing your todos efficiently. This API provides comprehensive endpoints for creating, reading, updating, and deleting todo items.
// @termsOfService  http://swagger.io/terms/

// @contact.name   API Support Team
// @contact.url    http://www.example.com/support
// @contact.email  support@example.com

// @license.name  MIT
// @license.url   https://opensource.org/licenses/MIT

// @host      localhost:8080
// @BasePath  /
// @schemes   http https

// @tag.name         todos
// @tag.description  Operations about todos
// @tag.docs.url     http://example.com/docs/todos
// @tag.docs.description Detailed information about todo operations

// @tag.name         health
// @tag.description  API health check operations

// @securityDefinitions.apikey Bearer
// @in header
// @name Authorization
// @description Enter the token with the `Bearer: ` prefix, e.g. "Bearer abcde12345".

func main() {
	var wg sync.WaitGroup

	app := fx.New(
		// Core Dependencies
		fx.Provide(
			NewConfig,
		),
		// Infrastructure Dependencies
		fx.Provide(
			database.NewDB,
			NewGinEngine,
			repository.NewRepository,
			elasticsearch.NewElasticsearchLogRepository,
			timescaledb.NewTimescaleMetricRepository,
			service.NewService,
			service.NewLogQueryService,
			service.NewMetricQueryService,
			service.NewNLVService,
			service.NewGeminiLLMService,
			controller.NewLogController,
			controller.NewMetricController,
			controller.NewNLVController,
			NewFileStateManager,
			parser.NewMultilineCapableParser,
			kafka.NewKafkaLogProducer,
			kafka.NewKafkaLogConsumer,
			elasticsearch.NewElasticLogStore,
			timescaledb.ProvideTimescaleDBPool,
			metrics.NewSparkLogExtractor,
			service.NewLogProducerService,
			service.NewLogConsumerService,
		),
		fx.Invoke(RegisterAPIRoutes,
			RegisterScheduler,
			func(lc fx.Lifecycle, consumerService service.LogConsumerService) { // Invoker to start consumer
				startLogConsumer(lc, &wg, consumerService)
			},
		),
	)

	// app.Run()

	startCtx, cancelStart := context.WithTimeout(context.Background(), 15*time.Second) // Timeout for startup
	defer cancelStart()
	if err := app.Start(startCtx); err != nil {
		log.Fatal().Err(err).Msg("Failed to start application")
	}
	<-app.Done()

	// Initiate shutdown
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 30*time.Second) // Timeout for graceful shutdown
	defer cancelStop()
	log.Info().Msg("Shutting down application...")
	if err := app.Stop(stopCtx); err != nil {
		log.Error().Err(err).Msg("Forced shutdown due to error or timeout")
	}

	// Wait for background goroutines (like the consumer) to finish
	log.Info().Msg("Waiting for background goroutines to finish...")
	wg.Wait() // Wait for consumer goroutine to complete
	log.Info().Msg("All background processes finished. Exiting.")
}

func NewConfig() (*config.Config, error) {
	return config.NewConfig()
}

func NewGinEngine() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// Configure CORS
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"}, // Add your frontend URLs
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// Add swagger route
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	return r
}

func RegisterAPIRoutes(
	lifecycle fx.Lifecycle,
	router *gin.Engine,
	cfg *config.Config,
	logController *controller.LogController,
	metricController *controller.MetricController,
	nlvController *controller.NLVController,
) {
	if logController != nil {
		controller.RegisterLogRoutes(router, logController)
	} else {
		log.Warn().Msg("LogController not provided, skipping log API routes.")
	}

	if metricController != nil {
		controller.RegisterMetricRoutes(router, metricController) // Gọi hàm đăng ký của metric controller
	} else {
		log.Warn().Msg("MetricController not provided")
	}
	if nlvController != nil {
		controller.RegisterNLVRoutes(router, nlvController)
	} else {
		log.Warn().Msg("NLVController not provided")
	}

	server := &http.Server{
		Addr:    ":" + cfg.Server.Port,
		Handler: router,
	}
	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.Info().Msgf("Starting HTTP server on port %s", cfg.Server.Port)
			go func() {
				if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Error().Err(err).Msg("HTTP server ListenAndServe error")
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info().Msg("Shutting down HTTP server...")
			return server.Shutdown(ctx)
		},
	})
}

// --- Factory Functions ---

func NewFileStateManager(cfg *config.Config) filestate.Manager {
	return filestate.NewManager(cfg.FileState.FilePath)
}

// --- Invoker Functions ---

func RegisterScheduler(lc fx.Lifecycle, cfg *config.Config, logProducerSvc service.LogProducerService) {
	scheduler.NewScheduler(lc, cfg, logProducerSvc)
}

// startLogConsumer starts the LogConsumerService in a goroutine managed by fx lifecycle
func startLogConsumer(lc fx.Lifecycle, wg *sync.WaitGroup, consumerService service.LogConsumerService) {
	wg.Add(1)
	ctx, cancel := context.WithCancel(context.Background()) // Create cancellable context

	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			log.Info().Msg("Starting Log Consumer goroutine")
			go consumerService.Run(ctx, wg) // Run in goroutine with cancellable context
			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			log.Info().Msg("Signaling Log Consumer goroutine to stop...")
			cancel()   // Cancel the context to signal the consumer loop to exit
			return nil // Return immediately, main WaitGroup handles the actual wait
		},
	})
}
