package main

import (
	"context"
	"flag"
	"github.com/cybroslabs/cloudflare-tunnel-ingress-controller/internal/controller"
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
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	logger := stdr.NewWithOptions(log.New(os.Stderr, "", log.LstdFlags), stdr.Options{LogCaller: stdr.All})
	stdr.SetVerbosity(0)

	logger.Info("logger verbosity", "verbosity", 0)

	loadConfig(logger)
	flag.Parse()

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

	err = controller.RegisterIngressController(logger, mgr, controller.IngressControllerOptions{
		IngressClassName:    ingressClassName,
		ControllerClassName: controllerClassName,
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
	flag.StringVar(&ingressClassName, "ingress-class-name", "cloudflare-tunnel", "Ingress class name to watch for")
	flag.StringVar(&controllerClassName, "controller-class-name", "clbs.io/cloudflare-tunnel-ingress-controller", "Controller class name to set on Ingress")
}
