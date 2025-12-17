package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
)

type IPInfoRecord struct {
	ASN           string `maxminddb:"asn"`
	ASName        string `maxminddb:"as_name"`
	ASDomain      string `maxminddb:"as_domain"`
	CountryCode   string `maxminddb:"country_code"`
	Country       string `maxminddb:"country"`
	ContinentCode string `maxminddb:"continent_code"`
	Continent     string `maxminddb:"continent"`
}

type ASNMeta struct {
	Name   string
	Domain string
}

type CCMeta struct {
	Country       string
	ContinentCode string
	Continent     string
}

func normalizeASN(asn string) string {
	asn = strings.TrimSpace(asn)
	if asn == "" {
		return "0"
	}
	if strings.HasPrefix(asn, "AS") || strings.HasPrefix(asn, "as") {
		asn = asn[2:]
	}
	asn = strings.TrimSpace(asn)
	if asn == "" {
		return "0"
	}
	return asn
}

func normalizeCC(cc string) string {
	cc = strings.TrimSpace(cc)
	if cc == "" {
		return "--"
	}
	return strings.ToUpper(cc)
}

// We use '|' as a field separator in map values, so ensure it can never appear inside fields.
// Also strip newlines to keep each record on one line.
func sanitizeField(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	s = strings.ReplaceAll(s, "|", "/")
	return s
}

type tempOut struct {
	tmpPath   string
	finalPath string
	f         *os.File
	w         *bufio.Writer
}

func newTempOut(dir, name string) (*tempOut, error) {
	final := filepath.Join(dir, name)
	tmp := final + ".tmp"

	f, err := os.Create(tmp)
	if err != nil {
		return nil, err
	}
	w := bufio.NewWriterSize(f, 128*1024)

	return &tempOut{
		tmpPath:   tmp,
		finalPath: final,
		f:         f,
		w:         w,
	}, nil
}

func (t *tempOut) CloseAndCommit() error {
	if err := t.w.Flush(); err != nil {
		_ = t.f.Close()
		return err
	}
	if err := t.f.Close(); err != nil {
		return err
	}
	return os.Rename(t.tmpPath, t.finalPath)
}

func main() {
	var (
		mmdbFile = flag.String("mmdb", "ipinfo_lite.mmdb", "Path to ipinfo lite MMDB file")

		outDir = flag.String("outdir", ".", "Output directory for HAProxy map files")

		ipMap  = flag.String("ip-map", "ip_to_meta.map", "CIDR -> <asn>|<country_code>")
		asnMap = flag.String("asn-map", "asn_to_meta.map", "ASN -> <as_name>|<as_domain>")
		ccMap  = flag.String("cc-map", "cc_to_meta.map", "CountryCode -> <country>|<continent_code>|<continent>")

		help = flag.Bool("help", false, "Show help")
	)
	flag.Parse()

	if *help {
		fmt.Println("mmdb2haproxymaps - Convert ipinfo lite MMDB to HAProxy map files")
		fmt.Println()
		flag.PrintDefaults()
		fmt.Println()
		fmt.Println("Generated map files:")
		fmt.Println("  ip_to_meta.map:  <cidr> <asn>|<country_code>")
		fmt.Println("  asn_to_meta.map: <asn>  <as_name>|<as_domain>")
		fmt.Println("  cc_to_meta.map:  <cc>   <country>|<continent_code>|<continent>")
		return
	}

	if _, err := os.Stat(*mmdbFile); os.IsNotExist(err) {
		log.Fatalf("MMDB file does not exist: %s", *mmdbFile)
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatalf("Failed to create outdir %s: %v", *outDir, err)
	}

	fmt.Printf("Opening MMDB file: %s\n", *mmdbFile)
	db, err := maxminddb.Open(*mmdbFile)
	if err != nil {
		log.Fatalf("Error opening MMDB file: %v", err)
	}
	defer db.Close()

	fmt.Printf("Database: %s\n", db.Metadata.DatabaseType)
	fmt.Printf("Node count: %d\n", db.Metadata.NodeCount)

	ipOut, err := newTempOut(*outDir, *ipMap)
	if err != nil {
		log.Fatalf("Error creating ip map output: %v", err)
	}
	asnOut, err := newTempOut(*outDir, *asnMap)
	if err != nil {
		_ = os.Remove(ipOut.tmpPath)
		log.Fatalf("Error creating asn map output: %v", err)
	}
	ccOut, err := newTempOut(*outDir, *ccMap)
	if err != nil {
		_ = os.Remove(ipOut.tmpPath)
		_ = os.Remove(asnOut.tmpPath)
		log.Fatalf("Error creating cc map output: %v", err)
	}

	defer func() {
		// In case of panic/early return, cleanup tmp files best-effort
		_ = os.Remove(ipOut.tmpPath)
		_ = os.Remove(asnOut.tmpPath)
		_ = os.Remove(ccOut.tmpPath)
	}()

	// Headers
	now := time.Now().Format(time.RFC3339)
	ipOut.w.WriteString("# Generated from: " + *mmdbFile + "\n")
	ipOut.w.WriteString("# Generated at: " + now + "\n")
	ipOut.w.WriteString("# Format: <cidr> <asn>|<country_code>\n\n")

	asnOut.w.WriteString("# Generated from: " + *mmdbFile + "\n")
	asnOut.w.WriteString("# Generated at: " + now + "\n")
	asnOut.w.WriteString("# Format: <asn> <as_name>|<as_domain>\n\n")

	ccOut.w.WriteString("# Generated from: " + *mmdbFile + "\n")
	ccOut.w.WriteString("# Generated at: " + now + "\n")
	ccOut.w.WriteString("# Format: <country_code> <country>|<continent_code>|<continent>\n\n")

	// Build small maps while streaming the large CIDR map
	asnMeta := make(map[string]ASNMeta, 8192)
	ccMeta := make(map[string]CCMeta, 512)

	count := 0
	start := time.Now()

	fmt.Println("Starting network processing...")
	// Track if we actually found data (helpful for debugging schema mismatches)
	recordsFound := 0

	for result := range db.Networks() {
		var rec IPInfoRecord
		if err := result.Decode(&rec); err != nil {
			log.Printf("Error decoding record: %v", err)
			continue
		}

		network := result.Prefix().String()
		asn := normalizeASN(rec.ASN)
		cc := normalizeCC(rec.CountryCode)

		// Debug check: If we parsed 10,000 networks and found no ASNs or CCs, warn user.
		if recordsFound == 0 && (asn != "0" || cc != "--") {
			recordsFound++
		}

		// Write CIDR -> asn|cc
		if asn != "0" || cc != "--" {
			ipOut.w.WriteString(network)
			ipOut.w.WriteString(" ")
			ipOut.w.WriteString(asn)
			ipOut.w.WriteString("|")
			ipOut.w.WriteString(cc)
			ipOut.w.WriteString("\n")
		}

		// Collect ASN metadata
		if asn != "0" {
			cur := asnMeta[asn]

			// Only sanitize if we are actually going to update
			if cur.Name == "" || cur.Name == "-" {
				cur.Name = sanitizeField(rec.ASName)
			}
			if cur.Domain == "" || cur.Domain == "-" {
				cur.Domain = sanitizeField(rec.ASDomain)
			}
			asnMeta[asn] = cur
		}

		// Collect CountryCode metadata
		if cc != "--" {
			cur := ccMeta[cc]

			if cur.Country == "" || cur.Country == "-" {
				cur.Country = sanitizeField(rec.Country)
			}
			if cur.ContinentCode == "" || cur.ContinentCode == "-" {
				cur.ContinentCode = sanitizeField(rec.ContinentCode)
			}
			if cur.Continent == "" || cur.Continent == "-" {
				cur.Continent = sanitizeField(rec.Continent)
			}
			ccMeta[cc] = cur
		}

		count++
		if count%250000 == 0 {
			fmt.Printf("Processed %d networks...\n", count)
		}
	}

	if count > 0 && recordsFound == 0 {
		log.Println("WARNING: Processed networks but found no ASN or Country data.")
		log.Println("Check that your MMDB file schema matches the struct tags (maxminddb:\"asn\", etc).")
	}

	// Write ASN map sorted numerically (nice-to-have)
	asnKeys := make([]int, 0, len(asnMeta))
	asnNonNumeric := make([]string, 0)
	for k := range asnMeta {
		if n, err := strconv.Atoi(k); err == nil {
			asnKeys = append(asnKeys, n)
		} else {
			asnNonNumeric = append(asnNonNumeric, k)
		}
	}
	sort.Ints(asnKeys)
	sort.Strings(asnNonNumeric)

	for _, n := range asnKeys {
		k := strconv.Itoa(n)
		v := asnMeta[k]
		asnOut.w.WriteString(k)
		asnOut.w.WriteString(" ")
		asnOut.w.WriteString(sanitizeField(v.Name))
		asnOut.w.WriteString("|")
		asnOut.w.WriteString(sanitizeField(v.Domain))
		asnOut.w.WriteString("\n")
	}
	for _, k := range asnNonNumeric {
		v := asnMeta[k]
		asnOut.w.WriteString(k)
		asnOut.w.WriteString(" ")
		asnOut.w.WriteString(sanitizeField(v.Name))
		asnOut.w.WriteString("|")
		asnOut.w.WriteString(sanitizeField(v.Domain))
		asnOut.w.WriteString("\n")
	}

	// Write CC map sorted alphabetically
	ccKeys := make([]string, 0, len(ccMeta))
	for k := range ccMeta {
		ccKeys = append(ccKeys, k)
	}
	sort.Strings(ccKeys)

	for _, k := range ccKeys {
		v := ccMeta[k]
		ccOut.w.WriteString(k)
		ccOut.w.WriteString(" ")
		ccOut.w.WriteString(sanitizeField(v.Country))
		ccOut.w.WriteString("|")
		ccOut.w.WriteString(sanitizeField(v.ContinentCode))
		ccOut.w.WriteString("|")
		ccOut.w.WriteString(sanitizeField(v.Continent))
		ccOut.w.WriteString("\n")
	}

	// Commit atomically (rename .tmp -> final)
	if err := ipOut.CloseAndCommit(); err != nil {
		log.Fatalf("Failed to commit %s: %v", ipOut.finalPath, err)
	}
	if err := asnOut.CloseAndCommit(); err != nil {
		log.Fatalf("Failed to commit %s: %v", asnOut.finalPath, err)
	}
	if err := ccOut.CloseAndCommit(); err != nil {
		log.Fatalf("Failed to commit %s: %v", ccOut.finalPath, err)
	}

	dur := time.Since(start)
	fmt.Printf("Completed! Processed %d networks in %v\n", count, dur)
	fmt.Printf("Wrote:\n  %s\n  %s\n  %s\n",
		filepath.Join(*outDir, *ipMap),
		filepath.Join(*outDir, *asnMap),
		filepath.Join(*outDir, *ccMap),
	)
}
