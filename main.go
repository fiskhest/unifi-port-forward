package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"kube-router-port-forward/handlers"
	"kube-router-port-forward/routers"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

var (
	routerIP = os.Getenv("UNIFI_ROUTER_IP")
	username = os.Getenv("UNIFI_USERNAME")
	password = os.Getenv("UNIFI_PASSWORD")
	site     = os.Getenv("UNIFI_SITE")
)

const (
	filterAnnotation = "kube-port-forward-controller/open"
)

// TODO: unused?
// var router routers.Router

func main() {
	if routerIP == "" {
		routerIP = "192.168.27.1"
	}

	baseURL := fmt.Sprintf("https://%s", routerIP)

	if site == "" {
		site = "default"
	}

	if username == "" {
		username = "admin"
	}

	router, err := routers.CreateUnifiRouter(baseURL, username, password, site)
	if err != nil {
		log.Fatalf("Creating router: %v\n", err)
	}

	// load in cluster config from service account
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	watchlist := cache.NewListWatchFromClient(
		clientset.CoreV1().RESTClient(),
		"services",
		metav1.NamespaceAll,
		fields.Everything(),
	)

	// Create service handler with dependencies
	serviceHandler := handlers.NewServiceHandler(router, router.Client, site, filterAnnotation)

	_, controller := cache.NewInformer(
		watchlist,
		&v1.Service{},
		time.Second*0,
		cache.ResourceEventHandlerFuncs{
			AddFunc:    serviceHandler.OnAdd,
			UpdateFunc: serviceHandler.OnUpdate,
			DeleteFunc: serviceHandler.OnDelete,
		},
	)

	stop := make(chan struct{})
	defer close(stop)
	go controller.Run(stop)

	// Wait forever
	select {}
}
