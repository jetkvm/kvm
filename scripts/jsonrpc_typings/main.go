package main

import (
	"flag"
	"os"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	// Parse command line flags
	logLevel := flag.String("log-level", "info", "Log level (trace, debug, info, warn, error, fatal, panic)")
	searchPath := flag.String("search-path", ".", "Path to search for Go files containing type definitions")
	outputPath := flag.String("output", "jsonrpc.ts", "Output path for the generated TypeScript file")
	flag.Parse()

	// Set log level
	level, err := zerolog.ParseLevel(strings.ToLower(*logLevel))
	if err != nil {
		log.Fatal().Err(err).Str("level", *logLevel).Msg("Invalid log level")
	}
	zerolog.SetGlobalLevel(level)

	// Configure zerolog for pretty console output
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// Create API schema
	log.Info().Str("search-path", *searchPath).Msg("Creating API schema from JSON-RPC handlers")
	schema := NewAPISchema(*searchPath)

	// Generate TypeScript typings
	log.Info().Msg("Generating TypeScript typings")
	typings := generateTypeScriptTypings(schema, *searchPath)

	// Write to output file
	log.Info().Str("file", *outputPath).Msg("Writing TypeScript definitions to file")
	err = os.WriteFile(*outputPath, []byte(typings), 0644)
	if err != nil {
		log.Fatal().Err(err).Str("file", *outputPath).Msg("Failed to write TypeScript definitions")
	}

	log.Info().Str("file", *outputPath).Msg("TypeScript typings generated successfully")
}
