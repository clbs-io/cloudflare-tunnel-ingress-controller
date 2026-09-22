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
	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/option"
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

	resyncPeriod time.Duration
	leaderElect  bool

	tunnelTokenSecret     string
	cloudflaredDeployment string

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

	mgr, err := manager.New(cfg, manager.Options{
		LeaderElection:                leaderElect,
		LeaderElectionID:              "cloudflare-tunnel-ingress-controller",
		LeaderElectionNamespace:       controller.Namespace(),
		LeaderElectionReleaseOnCancel: true,
	})
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

	if _, err := controller.RegisterIngressController(logger, mgr, controller.IngressControllerOptions{
		IngressClassName:      ingressClassName,
		ControllerClassName:   controllerClassName,
		ResyncPeriod:          resyncPeriod,
		TunnelClient:          tunnelClient,
		TunnelTokenSecret:     tunnelTokenSecret,
		CloudflaredDeployment: cloudflaredDeployment,
	}); err != nil {
		return fmt.Errorf("could not register ingress controller: %w", err)
	}

	if err := tunnelClient.EnsureTunnelExists(ctx, logger); err != nil {
		return fmt.Errorf("could not ensure tunnel exists: %w", err)
	}

	// Fails fast on an API token that cannot read the tunnel token
	if _, err := tunnelClient.GetTunnelToken(ctx); err != nil {
		return fmt.Errorf("could not get tunnel token: %w", err)
	}

	healthSrv := health.NewServer(logger.WithName("health"), 8081)

	// Whichever of the health server and the manager stops first cancels ctx,
	// so the other stops too and the process exits instead of staying live
	// without reconciling.
	var (
		wg         sync.WaitGroup
		health_err error
		mgr_err    error
	)

	wg.Go(func() {
		defer stop()
		if err := healthSrv.Start(ctx); err != nil {
			health_err = fmt.Errorf("health server: %w", err)
		}
	})

	wg.Go(func() {
		defer stop()
		if err := mgr.Start(ctx); err != nil {
			mgr_err = fmt.Errorf("manager: %w", err)
		}
	})

	// WaitForCacheSync returns false only once ctx is done.
	logger.Info("Waiting for cache to sync...")
	if mgr.GetCache().WaitForCacheSync(ctx) {
		healthSrv.SetReady(true)
		logger.Info("Controller is ready")
	}

	wg.Wait()
	logger.Info("Controller stopped", "cause", context.Cause(ctx))
	return errors.Join(mgr_err, health_err)
}

func loadConfig() error {
	flag.StringVar(&ingressClassName, "ingress-class-name", "cloudflare-tunnel", "Ingress class name to watch for")
	flag.StringVar(&controllerClassName, "controller-class-name", "clbs.io/cloudflare-tunnel-ingress-controller", "Controller class name to set on Ingress")
	flag.DurationVar(&resyncPeriod, "resync-period", 10*time.Minute, "Interval of the full reconcile when nothing changes")
	flag.BoolVar(&leaderElect, "leader-elect", true, "Elect a leader so only one replica reconciles")
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

	tunnelTokenSecret = os.Getenv("TUNNEL_TOKEN_SECRET")
	if tunnelTokenSecret == "" {
		return errors.New("TUNNEL_TOKEN_SECRET is required")
	}

	cloudflaredDeployment = os.Getenv("CLOUDFLARED_DEPLOYMENT")
	if cloudflaredDeployment == "" {
		return errors.New("CLOUDFLARED_DEPLOYMENT is required")
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
