# mmdb2map

A CLI tool that converts ipinfo lite MMDB (MaxMind database) into map files for use with nginx/haproxy.

## Overview

mmdb2map reads IP network data from MMDB files and generates two types of mappings:
- **ASN mappings**: IP networks to ASN numbers
- **Location mappings**: IP networks to country codes

The generated map files are compatible with nginx geo module and haproxy map functionality.

## Installation

```bash
go build -o mmdb2map main.go
```

## Getting the ipinfo lite MMDB DB 

Before using `mmdb2map`, you need to download the ipinfo lite DB. To use the included script, you can either:
1. **Set environment variable:**
```bash
export IPINFO_TOKEN="your_token"
./get_ipinfo_db.sh
```

2. **Edit the script directly:**
Update the `IPINFO_TOKEN` variable in `get_ipinfo_db.sh`

```bash
./get_ipinfo_db.sh
```

The script will:
- Download the latest MMDB file with a timestamp
- Backup the current database file (if it exists) to `.old`
- Replace the current database with the new one

## Usage
### Basic Usage
Run with default settings (uses ipinfo_lite.mmdb as input)
```bash
./mmdb2map
```

Run with custom MMDB file and output paths
```bash
./mmdb2map -mmdb custom.mmdb -asn-output custom_asn.map -loc-output custom_loc.map
```

Show help
```bash
./mmdb2map -help
```

### Command Line Options
- `-mmdb`: Input MMDB file path (default: `ipinfo_lite.mmdb`)
- `-asn-output`: ASN map output file path (default: `ip_to_asn.map`)
- `-loc-output`: Location map output file path (default: `ip_to_loc.map`)

## Output Format

### ASN Map File
```
<network> <asn>
```
Example:
```
1.0.0.0/24 13335
8.8.8.0/24 15169
```

### Location Map File
```
<network> <country_code>
```
Example:
```
1.0.0.0/24 AU
8.8.8.0/24 US
```

## Dependencies

- [github.com/oschwald/maxminddb-golang](https://github.com/oschwald/maxminddb-golang) - For MMDB file parsing

## Development
Run directly with go run
```bash
go run main.go
```

Get dependencies
```bash
go mod tidy
```

Format code
```bash
go fmt
```

Vet code
```bash
go vet
```

