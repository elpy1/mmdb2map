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

const (
	emptyASN   = "0"
	emptyCC    = "--"
	emptyField = "-"
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
		return emptyASN
	}
	upper := strings.ToUpper(asn)
	if strings.HasPrefix(upper, "AS") {
		asn = asn[2:]
	}
	asn = strings.TrimSpace(asn)
	if asn == "" {
		return emptyASN
	}
	return asn
}

func normalizeCC(cc string) string {
	cc = strings.TrimSpace(cc)
	if cc == "" {
		return emptyCC
	}
	return strings.ToUpper(cc)
}

// We use '|' as a field separator in map values, so ensure it can never appear inside fields.
// Also strip newlines to keep each record on one line.
var fieldSanitizer = strings.NewReplacer(
	"\r", " ",
	"\n", " ",
	"|", "/",
)

func sanitizeField(s string) string {
	s = fieldSanitizer.Replace(s)
	s = strings.TrimSpace(s)
	if s == "" {
		return emptyField
	}
	return s
}

type tempOut struct {
	tmpPath   string
	finalPath string
	f         *os.File
	w         *bufio.Writer
	err       error // first write error encountered
	committed bool
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

// write writes a string and records the first error encountered.
func (t *tempOut) write(s string) {
	if t.err != nil {
		return
	}
	_, t.err = t.w.WriteString(s)
}

// Cleanup removes the temp file if not committed.
func (t *tempOut) Cleanup() {
	if !t.committed {
		_ = os.Remove(t.tmpPath)
	}
}

// Rollback removes the final file if it was committed.
func (t *tempOut) Rollback() {
	if t.committed {
		_ = os.Remove(t.finalPath)
		t.committed = false
	}
}

func (t *tempOut) CloseAndCommit() error {
	if t.err != nil {
		_ = t.f.Close()
		return t.err
	}
	if err := t.w.Flush(); err != nil {
		_ = t.f.Close()
		return err
	}
	if err := t.f.Close(); err != nil {
		return err
	}
	if err := os.Rename(t.tmpPath, t.finalPath); err != nil {
		return err
	}
	t.committed = true
	return nil
}

func main() {
	var (
		mmdbFile = flag.String("mmdb", "ipinfo_lite.mmdb", "Path to ipinfo lite MMDB file")

		outDir = flag.String("outdir", ".", "Output directory for HAProxy and Nginx map files")
		format = flag.String("format", "haproxy", "Output format: 'haproxy' or 'nginx' (adds semicolons)")

		ipMap  = flag.String("ip-map", "ip_to_meta.map", "CIDR -> <asn>|<country_code>")
		asnMap = flag.String("asn-map", "asn_to_meta.map", "ASN -> <as_name>|<as_domain>")
		ccMap  = flag.String("cc-map", "cc_to_meta.map", "CountryCode -> <country>|<continent_code>|<continent>")

		quiet = flag.Bool("quiet", false, "Suppress progress output")
		help  = flag.Bool("help", false, "Show help")
	)
	flag.Parse()

	if *help {
		fmt.Println("mmdb2map - Convert ipinfo lite MMDB to HAProxy and Nginx map files")
		fmt.Println()
		flag.PrintDefaults()
		fmt.Println()
		fmt.Println("Generated map files:")
		fmt.Println("  ip_to_meta.map:  <cidr> <asn>|<country_code>")
		fmt.Println("  asn_to_meta.map: <asn>  <as_name>|<as_domain>")
		fmt.Println("  cc_to_meta.map:  <cc>   <country>|<continent_code>|<continent>")
		return
	}

	var lineSuffix string
	if *format == "nginx" {
		lineSuffix = ";\n"
	} else if *format == "haproxy" {
		lineSuffix = "\n"
	} else {
		log.Fatalf("Invalid format: %s. Must be 'haproxy' or 'nginx'", *format)
	}

	if _, err := os.Stat(*mmdbFile); os.IsNotExist(err) {
		log.Fatalf("MMDB file does not exist: %s", *mmdbFile)
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatalf("Failed to create outdir %s: %v", *outDir, err)
	}

	if !*quiet {
		fmt.Printf("Opening MMDB file: %s\n", *mmdbFile)
	}
	db, err := maxminddb.Open(*mmdbFile)
	if err != nil {
		log.Fatalf("Error opening MMDB file: %v", err)
	}
	defer db.Close()

	if !*quiet {
		fmt.Printf("Database: %s\n", db.Metadata.DatabaseType)
		fmt.Printf("Node count: %d\n", db.Metadata.NodeCount)
	}

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
		// Cleanup any uncommitted temp files
		ipOut.Cleanup()
		asnOut.Cleanup()
		ccOut.Cleanup()
	}()

	// Headers
	now := time.Now().Format(time.RFC3339)
	ipOut.write("# Generated from: " + *mmdbFile + "\n")
	ipOut.write("# Generated at: " + now + "\n")
	ipOut.write("# Format: <cidr> <asn>|<country_code>\n\n")

	asnOut.write("# Generated from: " + *mmdbFile + "\n")
	asnOut.write("# Generated at: " + now + "\n")
	asnOut.write("# Format: <asn> <as_name>|<as_domain>\n\n")

	ccOut.write("# Generated from: " + *mmdbFile + "\n")
	ccOut.write("# Generated at: " + now + "\n")
	ccOut.write("# Format: <country_code> <country>|<continent_code>|<continent>\n\n")

	// Build small maps while streaming the large CIDR map
	asnMeta := make(map[string]ASNMeta, 8192)
	ccMeta := make(map[string]CCMeta, 512)

	count := 0
	decodeErrors := 0
	start := time.Now()

	if !*quiet {
		fmt.Println("Starting network processing...")
	}
	// Track if we actually found data (helpful for debugging schema mismatches)
	foundData := false

	for result := range db.Networks() {
		var rec IPInfoRecord
		if err := result.Decode(&rec); err != nil {
			decodeErrors++
			if !*quiet {
				log.Printf("Error decoding record: %v", err)
			}
			continue
		}

		network := result.Prefix().String()
		asn := normalizeASN(rec.ASN)
		cc := normalizeCC(rec.CountryCode)

		// Debug check: Track if we found any ASNs or CCs.
		if !foundData && (asn != emptyASN || cc != emptyCC) {
			foundData = true
		}

		// Write CIDR -> asn|cc
		if asn != emptyASN || cc != emptyCC {
			ipOut.write(network)
			ipOut.write(" ")
			ipOut.write(asn)
			ipOut.write("|")
			ipOut.write(cc)
			ipOut.write(lineSuffix)
		}

		// Collect ASN metadata
		if asn != emptyASN {
			cur := asnMeta[asn]

			// Only sanitize if we are actually going to update
			if cur.Name == "" || cur.Name == emptyField {
				cur.Name = sanitizeField(rec.ASName)
			}
			if cur.Domain == "" || cur.Domain == emptyField {
				cur.Domain = sanitizeField(rec.ASDomain)
			}
			asnMeta[asn] = cur
		}

		// Collect CountryCode metadata
		if cc != emptyCC {
			cur := ccMeta[cc]

			if cur.Country == "" || cur.Country == emptyField {
				cur.Country = sanitizeField(rec.Country)
			}
			if cur.ContinentCode == "" || cur.ContinentCode == emptyField {
				cur.ContinentCode = sanitizeField(rec.ContinentCode)
			}
			if cur.Continent == "" || cur.Continent == emptyField {
				cur.Continent = sanitizeField(rec.Continent)
			}
			ccMeta[cc] = cur
		}

		count++
		if !*quiet && count%250000 == 0 {
			fmt.Printf("Processed %d networks...\n", count)
		}
	}

	if count > 0 && !foundData {
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
		asnOut.write(k)
		asnOut.write(" ")
		asnOut.write(v.Name)
		asnOut.write("|")
		asnOut.write(v.Domain)
		asnOut.write(lineSuffix)
	}
	for _, k := range asnNonNumeric {
		v := asnMeta[k]
		asnOut.write(k)
		asnOut.write(" ")
		asnOut.write(v.Name)
		asnOut.write("|")
		asnOut.write(v.Domain)
		asnOut.write(lineSuffix)
	}

	// Write CC map sorted alphabetically
	ccKeys := make([]string, 0, len(ccMeta))
	for k := range ccMeta {
		ccKeys = append(ccKeys, k)
	}
	sort.Strings(ccKeys)

	for _, k := range ccKeys {
		v := ccMeta[k]
		ccOut.write(k)
		ccOut.write(" ")
		ccOut.write(v.Country)
		ccOut.write("|")
		ccOut.write(v.ContinentCode)
		ccOut.write("|")
		ccOut.write(v.Continent)
		ccOut.write(lineSuffix)
	}

	// Commit atomically (rename .tmp -> final) with rollback on failure
	if err := ipOut.CloseAndCommit(); err != nil {
		log.Fatalf("Failed to commit %s: %v", ipOut.finalPath, err)
	}
	if err := asnOut.CloseAndCommit(); err != nil {
		ipOut.Rollback()
		log.Fatalf("Failed to commit %s: %v", asnOut.finalPath, err)
	}
	if err := ccOut.CloseAndCommit(); err != nil {
		ipOut.Rollback()
		asnOut.Rollback()
		log.Fatalf("Failed to commit %s: %v", ccOut.finalPath, err)
	}

	dur := time.Since(start)
	if !*quiet {
		fmt.Printf("Completed! Processed %d networks in %v\n", count, dur)
		fmt.Printf("Unique ASNs: %d, Unique countries: %d\n", len(asnMeta), len(ccMeta))
		if decodeErrors > 0 {
			fmt.Printf("Decode errors: %d\n", decodeErrors)
		}
		fmt.Printf("Wrote:\n  %s\n  %s\n  %s\n",
			filepath.Join(*outDir, *ipMap),
			filepath.Join(*outDir, *asnMap),
			filepath.Join(*outDir, *ccMap),
		)
	}
}
