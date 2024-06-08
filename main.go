package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/linode/linodego"
	"golang.org/x/oauth2"
)

type configKey struct{}

type Config struct {
	DomainsToUpdate []DomainToUpdate `json:"domains"`
	DefaultTTL      int              `json:"default_ttl"`
	IpFinderURL     string           `json:"ip_finder_url"`
	IPv6            bool             `json:"ipv6"`
	IPv4            bool             `json:"ipv4"`
}

type DomainToUpdate struct {
	DomainName string `json:"domain"`
	RecordName string `json:"record"`
}

func getConfig(fileName string) Config {
	fileStream, err := os.Open(fileName)
	if err != nil {
		log.Fatal("Can't open file!")
	}
	bytes, err := io.ReadAll(fileStream)
	if err != nil {
		log.Fatal("Error reading file")
	}
	var config Config

	err = json.Unmarshal(bytes, &config)
	if err != nil {
		log.Fatal("Error parsing file")
	}

	if !config.IPv4 && !config.IPv6 {
		config.IPv4 = true
		config.IPv6 = true
	}

	return config
}

// gets the current outgoing IP
func getMyIP(ctx context.Context, mode string) (string, error) {
	config := ctx.Value(configKey{}).(Config)
	if !config.IPv4 && mode == "tcp4" {
		return "", nil
	}
	if !config.IPv6 && mode == "tcp6" {
		return "", nil
	}

	var zeroDialer net.Dialer
	httpClient := &http.Client{}
	transport := http.DefaultTransport.(*http.Transport).Clone()
    transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
        return zeroDialer.DialContext(ctx, mode, addr)
    }
    httpClient.Transport = transport

	req, err := http.NewRequest("GET", config.IpFinderURL, nil)
	if err != nil {
		log.Println(err)
		return "", err
	}
	client := httpClient
	response, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}

	defer response.Body.Close()
	bytes, err := io.ReadAll(response.Body)
	if err != nil {
		log.Println(err)
		return "", err
	}
	ip := strings.TrimSuffix(string(bytes), "\n")
	if net.ParseIP(ip) == nil {
		log.Printf("Not a valid IP: %s\n", ip)
	}
	return ip, nil
}

// fetch domain matching the string 'domain'
func fetchDomain(ctx context.Context, linodeClient *linodego.Client, domain string) (*linodego.Domain, error) {
	domains, err := linodeClient.ListDomains(ctx, linodego.NewListOptions(0, ""))
	if err != nil {
		return nil, err
	}

	for _, entry := range domains {
		if entry.Domain == domain {
			return &entry, nil
		}
	}

	return nil, nil
}

// fetch DNS records for domain with id domainId
func fetchRecord(ctx context.Context, linodeClient *linodego.Client, domainId int, record string, recordType linodego.DomainRecordType) (*linodego.DomainRecord, error) {
	records, err := linodeClient.ListDomainRecords(ctx, domainId, nil)
	if err != nil {
		return nil, err
	}

	for _, entry := range records {
		if entry.Name == record && entry.Type == recordType {
			return &entry, nil
		}
	}

	return nil, nil
}

func updateRecord(ctx context.Context, linodeClient *linodego.Client, domainId int, domain string, record string, ipAddress string, recordType linodego.DomainRecordType) error {
	domainRecord, err := fetchRecord(ctx, linodeClient, domainId, record, recordType)
	if err != nil {
		log.Fatal(err)
	}
	config := ctx.Value(configKey{}).(Config)

	if domainRecord != nil {
		if domainRecord.Target == ipAddress {
			log.Printf("Record %s for domain %s is already up to date\n", record, domain)
			return nil
		} else {
			log.Printf("Updating record %s for domain %s with IP %s\n", record, domain, ipAddress)
			_, err := linodeClient.UpdateDomainRecord(ctx, domainId, domainRecord.ID, linodego.DomainRecordUpdateOptions{
				Name:   record,
				Target: ipAddress,
				Type:   recordType,
				TTLSec: config.DefaultTTL,
			})
			if err != nil {
				return fmt.Errorf("failed to update record %s", record)
			}
		}
	} else {
		// we need to create the record
		log.Printf("Creating record %s for domain %s with IP %s\n", record, domain, ipAddress)
		_, err := linodeClient.CreateDomainRecord(ctx, domainId, linodego.DomainRecordCreateOptions{
			Name:   record,
			Target: ipAddress,
			Type:   recordType,
			TTLSec: config.DefaultTTL,
		})
		if err != nil {
			return fmt.Errorf("failed to create record %s\n", record)
		}
	}
	return nil
}

func main() {
	var configFile string
	if len(os.Args) != 2 {
		configFile = "config.json"
	} else {
		configFile = os.Args[1]
	}
	apiKey, ok := os.LookupEnv("LINODE_TOKEN")

	if !ok {
		log.Fatal("API key not found, please set LINODE_TOKEN")
	}

	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: apiKey})

	oauth2Client := &http.Client{
		Transport: &oauth2.Transport{
			Source: tokenSource,
		},
	}

	linodeClient := linodego.NewClient(oauth2Client)

	config := getConfig(configFile)
	ctx := context.Background()
	ctx = context.WithValue(ctx, configKey{}, config)
	myIP, err4 := getMyIP(ctx, "tcp4")
	myIP6, err6 := getMyIP(ctx, "tcp6")
	for _, entry := range config.DomainsToUpdate {
		domain, err := fetchDomain(ctx, &linodeClient, entry.DomainName)
		if err != nil {
			log.Fatalf("Can't find domain %s.\n", entry.DomainName)
		}
		if config.IPv4 {
			if err4 == nil {
				updateRecord(ctx, &linodeClient, domain.ID, entry.DomainName, entry.RecordName, myIP, linodego.RecordTypeA)
			} else {
				log.Println("Error fetching IPv4 address")
				log.Println(err4)
			}
		}
		if config.IPv6 {
			if err6 == nil {
				updateRecord(ctx, &linodeClient, domain.ID, entry.DomainName, entry.RecordName, myIP6, linodego.RecordTypeAAAA)
			} else {
				log.Println("Error fetching IPv6 address")
				log.Println(err6)
			}
		}
	}
}
