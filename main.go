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

	return config
}

// gets the current outgoing IP
func getMyIP(ctx context.Context) string {
	config := ctx.Value(configKey{}).(Config)
	req, err := http.NewRequest("GET", config.IpFinderURL, nil)
	if err != nil {
		log.Fatal(err)
	}
	client := &http.Client{}
	response, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}

	defer response.Body.Close()
	bytes, err := io.ReadAll(response.Body)
	if err != nil {
		log.Fatal(err)
	}
	ip := strings.TrimSuffix(string(bytes), "\n")
	if net.ParseIP(ip) == nil {
		log.Fatalf("Not a valid IP: %s", ip)
	}
	return ip
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
func fetchRecord(ctx context.Context, linodeClient *linodego.Client, domainId int, record string) (*linodego.DomainRecord, error) {
	records, err := linodeClient.ListDomainRecords(ctx, domainId, nil)
	if err != nil {
		return nil, err
	}

	for _, entry := range records {
		if entry.Name == record && entry.Type == linodego.RecordTypeA {
			return &entry, nil
		}
	}

	return nil, nil
}

func updateRecord(ctx context.Context, linodeClient *linodego.Client, domainId int, domain string, record string, ipAddress string) error {
	domainRecord, err := fetchRecord(ctx, linodeClient, domainId, record)
	if err != nil {
		log.Fatal(err)
	}
	config := ctx.Value(configKey{}).(Config)

	if domainRecord != nil {
		if domainRecord.Target == ipAddress {
			return nil
		} else {
			fmt.Printf("Updating record %s for domain %s with IP %s\n", record, domain, ipAddress)
			_, err := linodeClient.UpdateDomainRecord(ctx, domainId, domainRecord.ID, linodego.DomainRecordUpdateOptions{
				Name:   record,
				Target: ipAddress,
				Type:   linodego.RecordTypeA,
				TTLSec: config.DefaultTTL,
			})
			if err != nil {
				return fmt.Errorf("failed to update record %s", record)
			}
		}
	} else {
		// we need to create the record
		fmt.Printf("Creating record %s for domain %s with IP %s\n", record, domain, ipAddress)
		_, err := linodeClient.CreateDomainRecord(ctx, domainId, linodego.DomainRecordCreateOptions{
			Name:   record,
			Target: ipAddress,
			Type:   linodego.RecordTypeA,
			TTLSec: config.DefaultTTL,
		})
		if err != nil {
			return fmt.Errorf("failed to create record %s\n", record)
		}
	}
	return nil
}

func main() {
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

	config := getConfig("config.json")
	ctx := context.Background()
	ctx = context.WithValue(ctx, configKey{}, config)
	myIP := getMyIP(ctx)
	for _, entry := range config.DomainsToUpdate {
		domain, err := fetchDomain(ctx, &linodeClient, entry.DomainName)
		if err != nil {
			log.Fatalf("Can't find domain %s.\n", entry.DomainName)
		}
		updateRecord(ctx, &linodeClient, domain.ID, entry.DomainName, entry.RecordName, myIP)
	}
}
