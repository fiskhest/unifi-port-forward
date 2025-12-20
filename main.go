package main

import (
	"log"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"kube-router-port-forward/controller"
	"kube-router-port-forward/routers"

	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	routerIP = os.Getenv("UNIFI_ROUTER_IP")
	username = os.Getenv("UNIFI_USERNAME")
	password = os.Getenv("UNIFI_PASSWORD")
	site     = os.Getenv("UNIFI_SITE")
	apiKey   = os.Getenv("UNIFI_API_KEY")
	debug    = os.Getenv("DEBUG")
)

func main() {
	loglevel := slog.LevelWarn
	if debug != "" {
		loglevel = slog.LevelDebug
	}

	// Set up controller-runtime logging with slog
	slogLogger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: loglevel,
	}))
	ctrlLogger := logr.FromSlogHandler(slogLogger.Handler())
	ctrllog.SetLogger(ctrlLogger)

	ctrlLogger.Info("Starting kube-port-forward-controller", "log_level", loglevel.String())

	setupGracefulShutdown()

	if routerIP == "" {
		routerIP = "192.168.27.1"
	}

	baseURL := url.URL{
		Scheme: "https",
		Host:   routerIP,
	}

	if site == "" {
		site = "default"
	}

	if username == "" {
		username = "admin"
	}

	router, err := routers.CreateUnifiRouter(baseURL.String(), username, password, site, apiKey)
	if err != nil {
		log.Fatalf("Failed to create router: %v\n", err)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:           runtime.NewScheme(),
		LeaderElection:   true,
		LeaderElectionID: "port-forward-controller",
	})
	if err != nil {
		log.Fatalf("Failed to create manager: %v\n", err)
	}

	if err := corev1.AddToScheme(mgr.GetScheme()); err != nil {
		log.Fatalf("Failed to add corev1 to scheme: %v\n", err)
	}

	reconciler := &controller.PortForwardReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Router: router,
	}

	if err := reconciler.SetupWithManager(mgr); err != nil {
		log.Fatalf("Failed to setup controller: %v\n", err)
	}

	ctrlLogger.Info("Starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Fatalf("Failed to start manager: %v\n", err)
	}
}

func setupGracefulShutdown() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Received shutdown signal, gracefully stopping...")
		os.Exit(0)
	}()
}
