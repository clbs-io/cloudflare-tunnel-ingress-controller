package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/controller"
	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/health"
	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/tunnel"
	"github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/option"
	"github.com/go-logr/logr"
	"go.uber.org/zap"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var (
	Version = "dev"
)

var (
	ingressClassName    string
	controllerClassName string

	cloudflaredImage           string
	cloudflaredImagePullPolicy string

	cloudflareAPIToken string

	cloudflareAccountID  string
	cloudflareTunnelName string
)

func main() {
	loggerOpts := &logzap.Options{
		Development: false,
		ZapOpts:     []zap.Option{zap.AddCaller()},
	}

	ctrl.SetLogger(logzap.New(logzap.UseFlagOptions(loggerOpts)))

	logger := ctrl.Log.WithName("main")

	logger.Info("Starting Cloudflare Tunnel Ingress Controller, version: "+Version, "version", Version)

	if err := loadConfig(); err != nil {
		logger.Error(err, "could not load config")
		os.Exit(1)
	}

	if err := run(logger); err != nil {
		logger.Error(err, "controller failed")
		os.Exit(1)
	}
}

func run(logger logr.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	cfg, err := config.GetConfig()
	if err != nil {
		return fmt.Errorf("could not get k8s config: %w", err)
	}

	mgr, err := manager.New(cfg, manager.Options{})
	if err != nil {
		return fmt.Errorf("could not create manager: %w", err)
	}

	cf_opts := []option.RequestOption{
		option.WithAPIToken(cloudflareAPIToken),
	}

	cloudflareAPI := cloudflare.NewClient(cf_opts...)
	if cloudflareAPI == nil {
		return errors.New("could not create cloudflare API client: NewClient returned nil")
	}

	tunnelClient := tunnel.NewClient(cloudflareAPI, cloudflareAccountID, cloudflareTunnelName, logger)

	ctrlr, err := controller.RegisterIngressController(logger, mgr, controller.IngressControllerOptions{
		IngressClassName:    ingressClassName,
		ControllerClassName: controllerClassName,
		TunnelClient:        tunnelClient,
		CloudflaredConfig: controller.CloudflaredConfig{
			CloudflaredImage:           cloudflaredImage,
			CloudflaredImagePullPolicy: cloudflaredImagePullPolicy,
		},
	})
	if err != nil {
		return fmt.Errorf("could not register ingress controller: %w", err)
	}

	err = tunnelClient.EnsureTunnelExists(ctx, logger)
	if err != nil {
		return fmt.Errorf("could not ensure tunnel exists: %w", err)
	}

	token, err := tunnelClient.GetTunnelToken(ctx)
	if err != nil {
		return fmt.Errorf("could not get tunnel token: %w", err)
	}
	ctrlr.SetTunnelToken(token)

	healthSrv := health.NewServer(logger.WithName("health"), 8081)

	var wg sync.WaitGroup

	wg.Go(func() {
		if err := healthSrv.Start(ctx); err != nil {
			logger.Error(err, "health server error")
		}
	})

	wg.Go(func() {
		if err := mgr.Start(ctx); err != nil {
			logger.Error(err, "could not start manager")
		}
	})

	logger.Info("Waiting for cache to sync...")
	for !mgr.GetCache().WaitForCacheSync(ctx) {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(100 * time.Millisecond):
		}
	}

	for {
		err = ctrlr.EnsureCloudflaredDeploymentExists(ctx, logger)
		if err != nil {
			logger.Error(err, "could not ensure cloudflared deployment exists")
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(1 * time.Second):
			}
			continue
		}
		break
	}

	healthSrv.SetReady(true)
	logger.Info("Controller is ready")

	wg.Wait()
	return nil
}

func loadConfig() error {
	flag.StringVar(&ingressClassName, "ingress-class-name", "cloudflare-tunnel", "Ingress class name to watch for")
	flag.StringVar(&controllerClassName, "controller-class-name", "clbs.io/cloudflare-tunnel-ingress-controller", "Controller class name to set on Ingress")
	flag.Parse()

	if tokenFile := os.Getenv("CLOUDFLARE_API_TOKEN_FILE"); tokenFile != "" {
		token, err := os.ReadFile(filepath.Clean(tokenFile))
		if err != nil {
			return fmt.Errorf("could not read CLOUDFLARE_API_TOKEN_FILE: %w", err)
		}
		cloudflareAPIToken = strings.TrimSpace(string(token))
	} else {
		cloudflareAPIToken = os.Getenv("CLOUDFLARE_API_TOKEN")
	}
	if cloudflareAPIToken == "" {
		return errors.New("CLOUDFLARE_API_TOKEN or CLOUDFLARE_API_TOKEN_FILE is required")
	}

	cloudflaredImage = os.Getenv("CLOUDFLARED_IMAGE")
	if cloudflaredImage == "" {
		return errors.New("CLOUDFLARED_IMAGE is required")
	}

	cloudflaredImagePullPolicy = os.Getenv("CLOUDFLARED_IMAGE_PULL_POLICY")
	if cloudflaredImagePullPolicy == "" {
		return errors.New("CLOUDFLARED_IMAGE_PULL_POLICY is required")
	}

	cloudflareAccountID = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
	if cloudflareAccountID == "" {
		return errors.New("CLOUDFLARE_ACCOUNT_ID is required")
	}

	cloudflareTunnelName = os.Getenv("CLOUDFLARE_TUNNEL_NAME")
	if cloudflareTunnelName == "" {
		return errors.New("CLOUDFLARE_TUNNEL_NAME is required")
	}

	return nil
}
