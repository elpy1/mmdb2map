# mmdb2map

A CLI tool that converts ipinfo lite MMDB (MaxMind database) into map files for use with nginx/haproxy.

## Overview

mmdb2map reads IP network data from MMDB files and generates three types of mappings:
- **IP to Meta mapping**: IP networks mapped to ASN and Country Code
- **ASN to Meta mapping**: ASN mapped to AS Name and Domain
- **Country Code to Meta mapping**: Country Code mapped to Country and Continent

The generated map files are compatible with HAProxy map functionality and nginx geo module.

## Installation

```bash
go build -o mmdb2map main.go
```

## Getting the ipinfo lite MMDB DB 

Before using `mmdb2map`, you need to download the ipinfo lite DB. To use the included script, set the `IPINFO_TOKEN` environment variable and run the script:

```bash
export IPINFO_TOKEN="your_token"
./get_ipinfo_db.sh
```

The script will:
- Download the latest MMDB file with a timestamp
- Backup the current database file (if it exists) to `.old`
- Replace the current database with the new one

## Usage
### Basic Usage
Run with default settings (uses ipinfo_lite.mmdb as input and outputs to the current directory):
```bash
./mmdb2map
```

Run with a custom MMDB file and output directory:
```bash
./mmdb2map -mmdb custom.mmdb -outdir /etc/haproxy/maps
```

Show help:
```bash
./mmdb2map -help
```

### Command Line Options
- `-mmdb`: Path to ipinfo lite MMDB file (default: `ipinfo_lite.mmdb`)
- `-outdir`: Output directory for HAProxy and Nginx map files (default: `.`)
- `-format`: Output format: `haproxy` or `nginx`. The `nginx` format appends a semicolon to the end of each mapping line (default: `haproxy`)
- `-ip-map`: CIDR -> <asn>|<country_code> output file name (default: `ip_to_meta.map`)
- `-asn-map`: ASN -> <as_name>|<as_domain> output file name (default: `asn_to_meta.map`)
- `-cc-map`: CountryCode -> <country>|<continent_code>|<continent> output file name (default: `cc_to_meta.map`)
- `-quiet`: Suppress progress output
- `-help`: Show help

## Output Format

### IP Map File (ip_to_meta.map)
```
<cidr> <asn>|<country_code>
```
Example:
```
1.0.0.0/24 13335|AU
8.8.8.0/24 15169|US
```

### ASN Map File (asn_to_meta.map)
```
<asn> <as_name>|<as_domain>
```
Example:
```
13335 CLOUDFLARENET|cloudflare.com
15169 GOOGLE|google.com
```

### Country Code Map File (cc_to_meta.map)
```
<country_code> <country>|<continent_code>|<continent>
```
Example:
```
AU Australia|OC|Oceania
US United States|NA|North America
```

## Proxy Configuration Examples

Since the values are combined using a pipe `|` delimiter (e.g., `15169|US`), you must extract the fields within your proxy configuration.

### HAProxy

HAProxy uses the `field` converter to easily split strings based on a delimiter.

```haproxy
frontend my_app
    bind *:80
    
    # 1. Lookup the IP (src) in the map. Store in a transaction variable.
    http-request set-var(txn.ip_meta) src,map_ip(/etc/haproxy/maps/ip_to_meta.map)
    
    # 2. Extract the ASN (field 1)
    http-request set-var(txn.asn) var(txn.ip_meta),field(1,|)
    
    # 3. Extract the Country Code (field 2)
    http-request set-var(txn.cc) var(txn.ip_meta),field(2,|)
    
    # Example: Block access based on country code
    http-request deny if { var(txn.cc) -m str RU }
    
    # Example: Pass data to the backend app
    http-request add-header X-Country-Code %[var(txn.cc)]
    http-request add-header X-ASN %[var(txn.asn)]

    default_backend servers
```

### Nginx

Nginx uses a two-step process: `geo` to find the IP metadata, and regex `map` blocks to split the data. *Note: You must generate the maps using `-format nginx`.*

```nginx
http {
    # 1. Look up the IP block using the geo module
    geo $remote_addr $ip_meta {
        default "0|--";
        include /etc/nginx/maps/ip_to_meta.map;
    }

    # 2. Extract the ASN using a regular expression map
    map $ip_meta $ip_asn {
        "~^(?<asn_match>[^|]+)\|" $asn_match;
        default "0";
    }

    # 3. Extract the Country Code using a regular expression map
    map $ip_meta $ip_cc {
        "~\|(?<cc_match>.*)$" $cc_match;
        default "--";
    }

    server {
        listen 80;

        location / {
            # Example: Block access based on country code
            if ($ip_cc = "RU") {
                return 403;
            }

            # Example: Pass data to the backend app
            proxy_set_header X-Country-Code $ip_cc;
            proxy_set_header X-ASN $ip_asn;

            proxy_pass http://backend;
        }
    }
}
```

## Dependencies

- [github.com/oschwald/maxminddb-golang](https://github.com/oschwald/maxminddb-golang) - For MMDB file parsing

## Development
Run directly with go run:
```bash
go run main.go
```

Get dependencies:
```bash
go mod tidy
```

Format code:
```bash
go fmt ./...
```

Vet code:
```bash
go vet ./...
```