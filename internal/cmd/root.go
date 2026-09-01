// Podplane <https://podplane.dev>
// Copyright The Podplane Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	cloudstorage "cloud.google.com/go/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/podplane/registry/internal/buildvars"
	"github.com/podplane/registry/pkg/registry"
	"github.com/podplane/registry/pkg/storage"
	"github.com/podplane/registry/pkg/storage/gcs"
	registrys3 "github.com/podplane/registry/pkg/storage/s3"
)

// Run parses configuration and runs the server until shutdown.
func Run(args []string) error {
	listenDefault := os.Getenv("REGISTRY_LISTEN")
	if listenDefault == "" {
		listenDefault = "127.0.0.1:5000"
	}
	providerDefault := os.Getenv("REGISTRY_PROVIDER")
	if providerDefault == "" {
		providerDefault = "s3"
	}
	pathStyleDefault := false
	if value := os.Getenv("REGISTRY_S3_PATH_STYLE"); value != "" {
		var err error
		pathStyleDefault, err = strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("parse REGISTRY_S3_PATH_STYLE: %w", err)
		}
	}

	flags := flag.NewFlagSet("registry", flag.ContinueOnError)
	listen := flags.String("listen", listenDefault, "HTTP listen address")
	provider := flags.String("provider", providerDefault, "object storage provider (s3 or gcs)")
	bucket := flags.String("bucket", os.Getenv("REGISTRY_BUCKET"), "object storage bucket (required)")
	region := flags.String("region", os.Getenv("AWS_REGION"), "AWS region")
	endpoint := flags.String("endpoint", os.Getenv("REGISTRY_S3_ENDPOINT"), "custom S3 endpoint")
	pathStyle := flags.Bool("path-style", pathStyleDefault, "use S3 path-style addressing")
	profile := flags.String("profile", os.Getenv("AWS_PROFILE"), "AWS shared configuration profile")
	version := flags.Bool("version", false, "print build metadata")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *version {
		fmt.Printf("registry %s build=%s commit=%s commit-date=%s branch=%s\n", buildvars.BuildVersion(), buildvars.BuildDate(), buildvars.CommitHash(), buildvars.CommitDate(), buildvars.CommitBranch())
		return nil
	}
	if *bucket == "" {
		return errors.New("--bucket or REGISTRY_BUCKET is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var store storage.Reader
	switch *provider {
	case "s3":
		var loadOptions []func(*config.LoadOptions) error
		if *region != "" {
			loadOptions = append(loadOptions, config.WithRegion(*region))
		}
		if *profile != "" {
			loadOptions = append(loadOptions, config.WithSharedConfigProfile(*profile))
		}
		cfg, err := config.LoadDefaultConfig(ctx, loadOptions...)
		if err != nil {
			return fmt.Errorf("load AWS configuration: %w", err)
		}
		client := awss3.NewFromConfig(cfg, func(options *awss3.Options) {
			options.UsePathStyle = *pathStyle
			if *endpoint != "" {
				options.BaseEndpoint = aws.String(*endpoint)
			}
		})
		store = registrys3.New(client, *bucket)
	case "gcs":
		client, err := cloudstorage.NewClient(ctx)
		if err != nil {
			return fmt.Errorf("load Google Cloud Storage configuration: %w", err)
		}
		defer func() { _ = client.Close() }()
		store = gcs.New(client, *bucket)
	default:
		return fmt.Errorf("unsupported storage provider %q", *provider)
	}
	handler, err := registry.New(store)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/v2", handler)
	mux.Handle("/v2/", handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(listener) }()
	slog.Info("registry listening", "address", listener.Addr().String(), "version", buildvars.BuildVersion())
	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
	}
	return nil
}
