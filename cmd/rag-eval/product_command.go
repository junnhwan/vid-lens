package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"time"

	"vid-lens/internal/eval"
)

func runProductCommand(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("rag-eval product", flag.ContinueOnError)
	dataset := flags.String("dataset", "", "private JSON array of versioned product cases; uses existing evaluation sessions")
	output := flags.String("output", "", "private result path (must not exist)")
	base := flags.String("base-url", "http://127.0.0.1:8080", "running product server")
	tokenEnv := flags.String("token-env", "VIDLENS_EVAL_TOKEN", "environment variable containing authorized bearer token")
	identity := flags.String("identity", "", "frozen experiment identity file (code/patch/data/profile/schema/budget hashes)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dataset == "" || *output == "" || *identity == "" {
		return errors.New("dataset, output and identity are required")
	}
	token := os.Getenv(*tokenEnv)
	if token == "" {
		return errors.New("evaluation token environment variable is empty")
	}
	data, err := os.ReadFile(*dataset)
	if err != nil {
		return err
	}
	var cases []eval.ProductCase
	if err = json.Unmarshal(data, &cases); err != nil {
		return err
	}
	if err = eval.ValidateProductCases(cases); err != nil {
		return err
	}
	identityData, err := os.ReadFile(*identity)
	if err != nil {
		return err
	}
	if !json.Valid(identityData) {
		return errors.New("identity must be JSON without credentials")
	}
	file, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	results, err := eval.RunProductCases(ctx, cases, (eval.HTTPProductExecutor{BaseURL: *base, Token: token}).Execute)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(data)
	report := struct {
		Version       string               `json:"version"`
		CreatedAt     time.Time            `json:"created_at"`
		DatasetSHA256 string               `json:"dataset_sha256"`
		Identity      json.RawMessage      `json:"identity"`
		Distribution  map[string]int       `json:"distribution"`
		Results       []eval.ProductResult `json:"results"`
	}{eval.ProductSchemaVersion, time.Now().UTC(), hex.EncodeToString(hash[:]), identityData, eval.ProductDistribution(results), results}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}
