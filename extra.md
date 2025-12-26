# Extra TODO
-------

--- now ---

- [x] move to spf13/cobra + flags and env vars builtin handling, add parsing in config
  - do subcmds work?
    - [x] make cleaner take input by flag instead of hardcoded
  - [ ] verify
  - [x] merge in cmd/service-debugger as subcmd too

- [x] remove builtin "node/transient IP" handling
  - [x] helpers/helpers.go isNodeIP()

- [x] are finalizers implemented
  - [ ] and working correctly?
- [x] tests for ensuring finalizer works

-- soon ---
- update PLAN.md

- [x] should main_test.go be moved elsewhere in the structure? It doesnt look like its shadowing main.go
- [x] why simple_test.go? Can we unify it somewhere?

- [x] CLI validateIP helper should use real ip parsing instead of strings.Contains etc

--- verification ---

- what goes on in `calculateDelta`
- what is the new `unified way of processing rules`?
- what happens when a service was prematurely annotated and then the controller is rolled out?
- what happens if the actual _service_ changes name? Do we update portforward rules accordingly?
- what happens when controller is removed, are all port rules removed?
- is there a possible bug if we have two port forward rules already exists
    - test-namespace/test-service:http
    - test-namespace/test-service:https
- and we match with strings.HasPrefix and dont check proto?

--- later ---
- add retrying in case of transient API errors?
- it seems we run delete then add instead of update when we change a port...?
- investigate any performance issues/nuking of router API if we scale up amount of services under management
- do we handle transient node ip / debouncing?
- can we introduce CRDs so we can manage port forwards for services not in kubernetes with kubernetes manifests?
- document manual firewall rules somewhere (zk?)

#### **3.1 Retry Logic for Cleanup**
- Port deletion should use retry logic like other operations
- Handle router connectivity issues during cleanup
- Log cleanup failures appropriately
