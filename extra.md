# Extra TODO
-------

--- now ---

--- verification ---

- what goes on in `calculateDelta`
- what is the new `unified way of processing rules`?

--- later ---
- add retrying in case of transient API errors?
- [x] it seems we run delete then add instead of update when we change a port...?
  - expected for some operations - only ip address change results in an update
- investigate any performance issues/nuking of router API if we scale up amount of services under management
- do we handle transient node ip / debouncing?
- can we introduce CRDs so we can manage port forwards for services not in kubernetes with kubernetes manifests?
- document manual firewall rules somewhere (zk?)

#### **3.1 Retry Logic for Cleanup**
- Port deletion should use retry logic like other operations
- Handle router connectivity issues during cleanup
- Log cleanup failures appropriately
