package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
)

func main() {
	cfg := elasticsearch.Config{
		Addresses: []string{"http://localhost:9200"},
	}
	es, err := elasticsearch.NewClient(cfg)
	if err != nil {
		log.Fatalf("Error creating Elasticsearch client: %v", err)
	}

	file, err := os.Open("event_templates.csv")
	if err != nil {
		log.Fatalf("Error opening CSV file: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = 2

	ctx := context.Background()

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Error reading CSV row: %v", err)
			continue
		}

		// Create document from CSV row
		doc := map[string]interface{}{
			"event_id": record[0],
			"template": record[1],
		}

		docJSON, err := json.Marshal(doc)
		if err != nil {
			log.Printf("Error marshaling document: %v", err)
			continue
		}

		req := esapi.IndexRequest{
			Index:      "event_templates",
			DocumentID: record[0],
			Body:       strings.NewReader(string(docJSON)),
			Refresh:    "true",
		}

		res, err := req.Do(ctx, es)
		if err != nil {
			log.Printf("Error indexing document %s: %v", record[0], err)
			continue
		}
		defer res.Body.Close()

		if res.IsError() {
			log.Printf("Error response from Elasticsearch for document %s: %s", record[0], res.String())
		} else {
			fmt.Printf("Indexed document %s successfully\n", record[0])
		}
	}

}
