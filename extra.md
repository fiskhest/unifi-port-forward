# Extra TODO
-------

--- now ---
- remove the "legacy portforward namestandard migration logic"
  - should be removed but verify in code

  * [ ] clean up PortKey

- is implementing a new logger package in logging/logger.go the idiomatic approach? should we not use slog or logr for controller runtime? 
  - [ ] wtf is logger.V(1).Info???? Why not log.FromContext()? 

- move to spf13/cobra + flags and env vars builtin handling, add parsing in config

- remove builtin "node/transient IP" handling
- remove protocol_normalizer.go + validation/ package

- are finalizers implemented and working correctly?
- tests for ensuring finalizer works

-- soon ---
- update PLAN.md

- should main_test.go be moved elsewhere in the structure? It doesnt look like its shadowing main.go
- why simple_test.go? Can we unify it somewhere?

- what goes on in `calculateDelta`
- what is the new `unified way of processing rules`?

--- verification ---
- what happens when a service was prematurely annotated and then the controller is rolled out?
- what happens if the actual _service_ changes name? Do we update portforward rules accordingly?
- what happens when controller is removed, are all port rules removed?
- is there a possible bug if we have two port forward rules already exists
    - test-namespace/test-service:http
    - test-namespace/test-service:https
- and we match with strings.HasPrefix and dont check proto?


--- later ---
- add retrying in case of transient API errors?
- investigate any performance issues/nuking of router API if we scale up amount of services under management


#### **3.1 Retry Logic for Cleanup**
- Port deletion should use retry logic like other operations
- Handle router connectivity issues during cleanup
- Log cleanup failures appropriately
