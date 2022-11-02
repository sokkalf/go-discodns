package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/linode/linodego"
	"golang.org/x/oauth2"
)

type Config struct {
	DomainsToUpdate []DomainToUpdate `json:"domains"`
	DefaultTTL      int              `json:"default_ttl"`
}

type DomainToUpdate struct {
	DomainName string `json:"domain"`
	RecordName string `json:"record"`
}

var config Config

func getConfig(fileName string) Config {
	fileStream, err := os.Open(fileName)
	if err != nil {
		log.Fatal("Can't open file!")
	}
	bytes, err := ioutil.ReadAll(fileStream)
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
func getMyIP() string {
	req, err := http.NewRequest("GET", "http://api.discombobulator.org/cgi-bin/ip.cgi", nil)
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
	return strings.TrimSuffix(string(bytes), "\n")
}

// fetch domain matching the string 'domain'
func fetchDomain(linodeClient *linodego.Client, domain string) (*linodego.Domain, error) {
	domains, err := linodeClient.ListDomains(context.Background(), linodego.NewListOptions(0, ""))
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
func fetchRecord(linodeClient *linodego.Client, domainId int, record string) (*linodego.DomainRecord, error) {
	records, err := linodeClient.ListDomainRecords(context.Background(), domainId, nil)
	if err != nil {
		return nil, err
	}

	for _, entry := range records {
		if entry.Name == record {
			return &entry, nil
		}
	}

	return nil, nil
}

func updateRecord(linodeClient *linodego.Client, domainId int, record string, ipAddress string) error {
	domainRecord, err := fetchRecord(linodeClient, domainId, record)
	if err != nil {
		log.Fatal(err)
	}
	if domainRecord != nil {
		if domainRecord.Target == ipAddress {
			fmt.Printf("Record already up to date\n")
			return nil
		} else {
			_, err := linodeClient.UpdateDomainRecord(context.Background(), domainId, domainRecord.ID, linodego.DomainRecordUpdateOptions{
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
		_, err := linodeClient.CreateDomainRecord(context.Background(), domainId, linodego.DomainRecordCreateOptions{
			Name:   record,
			Target: ipAddress,
			Type:   linodego.RecordTypeA,
			TTLSec: config.DefaultTTL,
		})
		if err != nil {
			return fmt.Errorf("failed to create record %s", record)
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

	myIP := getMyIP()
	config = getConfig("config.json")

	for _, entry := range config.DomainsToUpdate {
		fmt.Printf("Updating %s.%s to %s\n", entry.RecordName, entry.DomainName, myIP)
		domain, err := fetchDomain(&linodeClient, entry.DomainName)
		if err != nil {
			log.Fatalf("Can't find domain %s.\n", entry.DomainName)
		}
		updateRecord(&linodeClient, domain.ID, entry.RecordName, myIP)
	}
}
