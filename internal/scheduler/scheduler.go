package scheduler

import (
	"context"
	"skeleton-internship-backend/config"
	"skeleton-internship-backend/internal/service"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"
	"go.uber.org/fx"
)

func NewScheduler(lc fx.Lifecycle, cfg *config.Config, logProducerSvc service.LogProducerService) *cron.Cron {
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.DowOptional | cron.Descriptor)
	c := cron.New(cron.WithParser(parser))

	schedule := cfg.LogProcessor.Schedule
	_, err := c.AddFunc(schedule, func() {
		go func() {
			err := logProducerSvc.ProcessLogs(context.Background())
			if err != nil {
				log.Error().Err(err).Msg("Error during scheduled log processing")
			}
		}()
	})

	if err != nil {
		log.Fatal().Err(err).Str("schedule", schedule).Msg("Failed to add cron job")
		return nil
	}
	log.Info().Str("schedule", schedule).Msg("Scheduled log processing job")

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.Info().Msg("Starting cron scheduler")
			c.Start()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info().Msg("Stopping cron scheduler...")
			stopCtx := c.Stop()
			select {
			case <-stopCtx.Done():
				log.Info().Msg("Cron scheduler stopped gracefully.")
				return nil
			case <-ctx.Done():
				log.Error().Msg("Context cancelled while waiting for cron scheduler to stop.")
				return ctx.Err()
			}
		},
	})

	return c
}
