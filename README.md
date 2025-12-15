# kube-port-forward-controller
Kubernetes controller for automatically configuring router settings to port forward services. 

This came out of the desire to completley provision an external service in my kubernetes homelab, using other services DNS can be automatically configured, the Load Balancer created, but no method to automatically open a port on my router, something frequently done when hosting things like game servers. 

### Supported Routers: 
- Unifi Dream Machine Pro (Though it's my understanding that it should work on any Unifi OS router, this is untested)

### Planned Supported Routers
- PfSense
- OPNSense
- VyOS
