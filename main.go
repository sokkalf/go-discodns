package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/linode/linodego"
	"golang.org/x/oauth2"
)

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
	//linodeClient.SetDebug(true)

	res, err := fetchDomain(&linodeClient, "zardoz.no") // linodeClient.ListDomains(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%v", res.ID)
}
