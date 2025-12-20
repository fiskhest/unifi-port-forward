# Port Forward Cleaner

A standalone utility for cleaning up specific port forwarding rules from UniFi routers. This tool is designed to remove manual or stale port forwarding rules that may have been created outside of the automatic kube-port-forward-controller.

## Overview

The cleaner connects to a UniFi router and removes specific port forwarding rules based on a predefined mapping of destination ports to destination IPs. This is useful for:

- Cleaning up manual port forwarding rules
- Removing stale rules from deleted services
- Resetting port configurations during maintenance
- Bulk removal of specific forwarding rules

## Environment Variables

The cleaner uses the same environment variables as the main controller:

- `UNIFI_ROUTER_IP`: IP address of the UniFi router (default: `192.168.27.1`)
- `UNIFI_USERNAME`: Router username (default: `kube-port-forward-controller`)
- `UNIFI_PASSWORD`: Router password (required)
- `UNIFI_SITE`: UniFi site name (default: `default`)

## Building and Running

### Build
```bash
cd cleaner/
go build -o cleaner .
```

### Run
```bash
# Set environment variables
export UNIFI_ROUTER_IP="192.168.1.1"
export UNIFI_USERNAME="admin"
export UNIFI_PASSWORD="your_password"
export UNIFI_SITE="default"

# Run the cleaner
./cleaner
```

### Docker
```bash
docker run --rm \
  -e UNIFI_ROUTER_IP="192.168.1.1" \
  -e UNIFI_USERNAME="admin" \
  -e UNIFI_PASSWORD="your_password" \
  -e UNIFI_SITE="default" \
  kube-port-forward-controller-cleaner
```

## Configuration

The cleaner uses a hardcoded mapping of destination ports to destination IPs. By default, it's configured to remove:

```go
portMaps := map[string]string{
    "83": "192.168.27.130",
}
```

### Customizing Port Mappings

To customize the port mappings, edit the `main.go` file and modify the `portMaps` variable:

```go
// Example: Remove multiple port forwarding rules
portMaps := map[string]string{
    "80":  "192.168.27.130",  // Remove HTTP forwarding
    "443": "192.168.27.130",  // Remove HTTPS forwarding
    "8080": "192.168.27.131", // Remove app forwarding
    "3306": "192.168.27.132", // Remove database forwarding
}
```

## Usage Examples

### Basic Cleanup
Remove the default configured port forwarding rule:
```bash
./cleaner
```

### Multiple Port Cleanup
After customizing the port mappings, remove multiple rules:
```bash
export UNIFI_PASSWORD="secure_password"
./cleaner
```

Output:
```
Logged in, UniFi version: 8.0.24
{ID:abc123 Fwd:192.168.27.130 FwdPort:83 DstPort:83 Protocol:tcp Enabled:true}
port matched
deleted port-forward rule ID abc123 successfully
```

### Dry Run Approach
To see what would be deleted without actually deleting, you can temporarily comment out the deletion line:

```go
// Comment out this line for dry run
// err := client.DeletePortForward(ctx, site, portforward.ID)
fmt.Printf("WOULD DELETE port-forward rule ID %s\n", portforward.ID)
```

## Integration with Main Controller

The cleaner complements the main kube-port-forward-controller:

1. **Controller creates rules automatically** based on Kubernetes service annotations
2. **Cleaner removes specific rules manually** when needed
3. Both use the same UniFi connection parameters
4. Cleaner can remove rules that were created manually or are no longer needed

### Typical Workflow

1. Deploy a service with port forwarding annotation (controller creates rules)
2. Service is deleted but rule persists (stale rule)
3. Use cleaner to remove the specific stale rule
4. Or use cleaner to bulk remove rules during maintenance

## Troubleshooting

### Common Issues

**Authentication failed:**
```
failed to create UniFi client: authentication failed
```
- Verify `UNIFI_USERNAME` and `UNIFI_PASSWORD`
- Check that the user has admin privileges on the UniFi controller

**Router connection failed:**
```
failed to create UniFi client: connection refused
```
- Verify `UNIFI_ROUTER_IP` is correct
- Check network connectivity to the router
- Ensure HTTPS is enabled on the UniFi controller

**No matching rules found:**
```
Logged in, UniFi version: 8.0.24
```
- Check if the port mappings match existing rules
- Verify destination IP and port combinations
- Use UniFi controller web UI to verify existing rules

**SSL verification errors:**
The cleaner disables SSL verification by default (`VerifySSL: false`). If you encounter SSL issues, ensure your UniFi controller has a valid certificate or keep SSL verification disabled.

### Debug Mode

To see what rules exist before attempting deletion:

1. List current port forwarding rules via UniFi controller web UI
2. Compare with the `portMaps` configuration in the cleaner
3. Ensure destination ports and IPs match exactly

### Safety Considerations

- **Backup before cleaning**: Take note of existing rules before running the cleaner
- **Test in development**: Test with non-production rules first
- **Specific mappings**: Only include the exact port/IP combinations you want to remove
- **Review output**: The cleaner prints each matching rule before deletion

## Security

- Store router credentials securely (environment variables, secrets management)
- Use dedicated service accounts with minimal required privileges
- Consider running the cleaner in a controlled environment
- Review logs after execution to ensure only intended rules were removed

## Permissions

The cleaner requires admin access to the UniFi controller to:
- List existing port forwarding rules
- Delete specific port forwarding rules

Ensure the configured user has these permissions on the target UniFi site.