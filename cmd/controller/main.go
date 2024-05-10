package main

import (
	"context"
	"flag"
	"github.com/cloudflare/cloudflare-go"
	"github.com/cybroslabs/cloudflare-tunnel-ingress-controller/internal/controller"
	"github.com/cybroslabs/cloudflare-tunnel-ingress-controller/internal/tunnel"
	"github.com/go-logr/logr"
	"github.com/go-logr/stdr"
	"log"
	"os"
	"os/signal"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"syscall"
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
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	logger := stdr.NewWithOptions(log.New(os.Stderr, "", log.LstdFlags), stdr.Options{LogCaller: stdr.All})
	stdr.SetVerbosity(0)

	logger.Info("logger verbosity", "verbosity", 0)

	loadConfig(logger)

	cfg, err := config.GetConfig()
	if err != nil {
		logger.Error(err, "could not get k8s config")
		os.Exit(1)
	}

	mgr, err := manager.New(cfg, manager.Options{})
	if err != nil {
		logger.Error(err, "could not create manager")
		os.Exit(1)
	}

	cloudflareAPI, err := cloudflare.NewWithAPIToken(cloudflareAPIToken)
	if err != nil {
		logger.Error(err, "could not create cloudflare API client")
		os.Exit(1)
	}

	tunnelClient := tunnel.NewClient(cloudflareAPI, cloudflareAccountID, cloudflareTunnelName, logger)

	err = controller.RegisterIngressController(logger, mgr, controller.IngressControllerOptions{
		IngressClassName:    ingressClassName,
		ControllerClassName: controllerClassName,
		TunnelClient:        tunnelClient,
		CloudflaredConfig: controller.CloudflaredConfig{
			CloudflaredImage:           cloudflaredImage,
			CloudflaredImagePullPolicy: cloudflaredImagePullPolicy,
		},
	})
	if err != nil {
		logger.Error(err, "could not register ingress controller")
		os.Exit(1)
	}

	err = mgr.Start(ctx)
	if err != nil {
		logger.Error(err, "could not start manager")
		os.Exit(1)
	}
}

func loadConfig(logger logr.Logger) {
	defer flag.Parse()

	flag.StringVar(&ingressClassName, "ingress-class-name", "cloudflare-tunnel", "Ingress class name to watch for")
	flag.StringVar(&controllerClassName, "controller-class-name", "clbs.io/cloudflare-tunnel-ingress-controller", "Controller class name to set on Ingress")

	cloudflareAPIToken = os.Getenv("CLOUDFLARE_API_TOKEN")
	if cloudflareAPIToken == "" {
		logger.Error(nil, "CLOUDFLARE_API_TOKEN is required")
		os.Exit(1)
	}

	cloudflaredImage = os.Getenv("CLOUDFLARED_IMAGE")
	if cloudflaredImage == "" {
		logger.Error(nil, "CLOUDFLARED_IMAGE is required")
		os.Exit(1)
	}

	cloudflaredImagePullPolicy = os.Getenv("CLOUDFLARED_IMAGE_PULL_POLICY")
	if cloudflaredImagePullPolicy == "" {
		logger.Error(nil, "CLOUDFLARED_IMAGE_PULL_POLICY is required")
		os.Exit(1)
	}

	cloudflareAccountID = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
	if cloudflareAccountID == "" {
		logger.Error(nil, "CLOUDFLARE_ACCOUNT_ID is required")
		os.Exit(1)
	}

	cloudflareTunnelName = os.Getenv("CLOUDFLARE_TUNNEL_NAME")
	if cloudflareTunnelName == "" {
		logger.Error(nil, "CLOUDFLARE_TUNNEL_NAME is required")
		os.Exit(1)
	}
}
