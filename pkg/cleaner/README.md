# Port Forward Cleaner

A CLI command for cleaning up specific port forwarding rules from UniFi routers. This tool removes stale or manual port forwarding rules based on configurable port mappings.

## Overview

The cleaner is integrated into the main `kube-port-forward-controller` binary as a `clean` command. It connects to a UniFi router and removes specific port forwarding rules based on provided port mappings.

## Usage

### CLI Command

```bash
./kube-port-forward-controller clean [flags]
```

### Required Flags

- `--port-mappings, -m`: Port mappings to clean (format: 'external-port:dest-ip', comma-separated)
- `--port-mappings-file, -f`: Path to port mappings configuration file (YAML/JSON)

**Either `--port-mappings` or `--port-mappings-file` is required.**

### Examples

#### Single Port Mapping (CLI Flag)
```bash
./kube-port-forward-controller clean \
  --port-mappings="83:192.168.27.130" \
  --password "your_password"
```

#### Multiple Port Mappings (CLI Flag)
```bash
./kube-port-forward-controller clean \
  --port-mappings="83:192.168.27.130,8080:192.168.27.131,443:192.168.27.132" \
  --password "your_password"
```

#### Port Mappings from File (YAML)
```bash
# Create port-mappings.yaml
cat > port-mappings.yaml <<EOF
mappings:
  - external-port: "83"
    destination-ip: "192.168.27.130"
  - external-port: "8080"
    destination-ip: "192.168.27.131"
  - external-port: "443"
    destination-ip: "192.168.27.132"
EOF

./kube-port-forward-controller clean \
  --port-mappings-file="port-mappings.yaml" \
  --password "your_password"
```

#### Port Mappings from File (JSON)
```bash
# Create port-mappings.json
cat > port-mappings.json <<EOF
{
  "mappings": [
    {
      "external-port": "83",
      "destination-ip": "192.168.27.130"
    },
    {
      "external-port": "8080", 
      "destination-ip": "192.168.27.131"
    }
  ]
}
EOF

./kube-port-forward-controller clean \
  --port-mappings-file="port-mappings.json" \
  --password "your_password"
```

#### With Environment Variables
```bash
export UNIFI_ROUTER_IP="192.168.1.1"
export UNIFI_USERNAME="admin"
export UNIFI_PASSWORD="your_password"
export UNIFI_SITE="default"

./kube-port-forward-controller clean \
  --port-mappings="80:192.168.27.130,443:192.168.27.130"
```

## Configuration

### Port Mapping Format

#### CLI String Format
- Format: `"external-port:destination-ip,external-port2:destination-ip2"`
- External port must be a valid port number (1-65535)
- Destination IP must be a valid IPv4/IPv6 address
- Multiple mappings separated by commas
- No spaces required (but spaces around commas are tolerated)

#### File Format
Both YAML and JSON formats support the same structure:

```yaml
# port-mappings.yaml
mappings:
  - external-port: "83"          # External port number
    destination-ip: "192.168.27.130"  # Destination IP address
  - external-port: "8080"
    destination-ip: "192.168.27.131"
```

```json
// port-mappings.json
{
  "mappings": [
    {
      "external-port": "83",
      "destination-ip": "192.168.27.130"
    },
    {
      "external-port": "8080", 
      "destination-ip": "192.168.27.131"
    }
  ]
}
```

## Integration with Main Controller

The cleaner complements the main kube-port-forward-controller:

1. **Controller creates rules automatically** based on Kubernetes service annotations
2. **Cleaner removes specific rules manually** when needed
3. Both use the same UniFi connection parameters and authentication
4. Cleaner can remove rules created manually or that are no longer needed

### Typical Workflow

1. Deploy a service with port forwarding annotation (controller creates rules)
2. Service is deleted but rule persists (stale rule)
3. Use cleaner to remove the specific stale rule
4. Use cleaner during maintenance to clean up multiple rules

## Troubleshooting

### Common Issues

**Missing required flag:**
```
Error: --port-mappings or --port-mappings-file is required
```
- Ensure you provide either `--port-mappings` or `--port-mappings-file`

**Invalid port format:**
```
Error: failed to parse port mappings: invalid port number: abc
```
- External ports must be numeric (1-65535)

**Invalid IP format:**
```
Error: failed to parse port mappings: invalid IP address: 192.168.27.x
```
- Destination IPs must be valid IPv4/IPv6 addresses

**File not found:**
```
Error: failed to parse port mappings: failed to read file: no such file or directory
```
- Ensure the file path is correct and the file exists

**File parsing error:**
```
Error: failed to parse port mappings: failed to parse file: yaml: line 5: mapping values are not allowed in this context
```
- Ensure YAML/JSON syntax is correct

### Debug Mode

To see what rules exist before attempting deletion:

1. List current port forwarding rules via UniFi controller web UI
2. Compare with your port mappings configuration
3. Ensure destination ports and IPs match exactly

### Safety Considerations

- **Backup before cleaning**: Take note of existing rules before running the cleaner
- **Test in development**: Test with non-production rules first
- **Specific mappings**: Only include the exact port/IP combinations you want to remove
- **Review output**: The cleaner prints each matching rule before deletion

## Environment Variables

All standard UniFi connection environment variables are supported:

- `UNIFI_ROUTER_IP`: IP address of UniFi router
- `UNIFI_USERNAME`: Router username 
- `UNIFI_PASSWORD`: Router password (required)
- `UNIFI_SITE`: UniFi site name
- `UNIFI_API_KEY`: API key (alternative to username/password)

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