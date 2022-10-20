package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/u-root/u-root/pkg/boot"
	"github.com/u-root/u-root/pkg/boot/bootcmd"
	"github.com/u-root/u-root/pkg/boot/menu"
	"github.com/u-root/u-root/pkg/boot/netboot"
	"github.com/u-root/u-root/pkg/curl"
	"github.com/u-root/u-root/pkg/dhclient"
	"github.com/u-root/u-root/pkg/ulog"
	"github.com/vishvananda/netlink"
)

const rhSSOTokenUrl = "https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/token"

var (
	apiURL           = flag.String("api-url", "https://api.openshift.com/api", "The url of the api-server")
	tokenFile        = flag.String("token-file", "", "A file containing he bearer tokenFile authorizing the api calls")
	refreshTokenFile = flag.String("refresh-token-file", "", "A file containing the refresh token to obtain a token file")
	infraEnvIDFile   = flag.String("infra-env-id-file", "", "A file containing the ID of the infraenv object")

	httpClient = &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}}
)

func main() {
	flag.Parse()
	if (*tokenFile == "" && *refreshTokenFile == "") || *infraEnvIDFile == "" {
		log.Fatalf("specify one of tokenFile or refreshTokenFile and infraEnvIDFile. Pass -tokenFile /file and -inrfaEnvID /file")
	}

	log.Printf("Run dhclient...\n")
	filteredIfs, err := dhclient.Interfaces("^e.")
	if err != nil {
		log.Fatal(err)
	}

	configureAll(filteredIfs)

	//https://api.openshift.com/api/assisted-install/v2/infra-envs/0886793b-19e6-408b-bb37-9596a29a5fd0/downloads/files?file_name=ipxe-script
	data, err := os.ReadFile(*infraEnvIDFile)
	if err != nil {
		log.Fatalf("failed to read infraEnvIDFile %s: %v", *infraEnvIDFile, err)
	}
	infraEnvID := strings.TrimSuffix(string(data), "\n")
	ipxescriptUrl := fmt.Sprintf("%s/assisted-install/v2/infra-envs/%s/downloads/files?file_name=ipxe-script", *apiURL, infraEnvID)

	token, err := getToken()
	if err != nil {
		log.Fatalf("failed getting the token %v", err)
	}
	os.Setenv("CURL_GET_HDR_Authorization", fmt.Sprintf("Bearer %s", token))

	var images []boot.OSImage

	var l dhclient.Lease
	l, err = newManualLease(ipxescriptUrl, filteredIfs[0])
	if err != nil {
		log.Fatal(err)
	}
	images, err = netboot.BootImages(context.Background(), ulog.Log, curl.DefaultSchemes, l)
	if err != nil {
		log.Printf("Netboot failed: %v", err)
	}

	verbose := true
	var menuEntries = menu.OSImages(verbose, images...)
	menuEntries = append(menuEntries, menu.Reboot{})
	menuEntries = append(menuEntries, menu.StartShell{})

	// Boot does not return.
	noLoad := false
	noExec := false
	bootcmd.ShowMenuAndBoot(menuEntries, nil, noLoad, noExec)
}

func getToken() (string, error) {
	if *tokenFile != "" {
		data, err := os.ReadFile(*tokenFile)
		if err != nil {
			return "", fmt.Errorf("failed reading the token file %s: %e", *tokenFile, err)
		}
		return strings.TrimSuffix(string(data), "\n"), nil
	} else {
		data, err := os.ReadFile(*refreshTokenFile)
		if err != nil {
			return "", fmt.Errorf("failed reading the refresh token file %s: %e", refreshTokenFile, err)
		}
		t := strings.TrimSuffix(string(data), "\n")
		return accessTokenFromRefresh(t, rhSSOTokenUrl)
	}
}

func newManualLease(ipxeScript string, link netlink.Link) (dhclient.Lease, error) {
	d, err := dhcpv4.New()
	if err != nil {
		return nil, err
	}

	d.BootFileName = ipxeScript
	d.ServerIPAddr = net.ParseIP("0.0.0.0")

	return dhclient.NewPacket4(link, d), nil
}

func configureAll(ifs []netlink.Link) {
	packetTimeout := 15 * time.Second

	retry := 5
	v4Port := 67
	c := dhclient.Config{
		Timeout: packetTimeout,
		Retries: retry,
		V4ServerAddr: &net.UDPAddr{
			IP:   net.IPv4bcast,
			Port: v4Port,
		},
	}
	ipv4 := true
	ipv6 := false
	r := dhclient.SendRequests(context.Background(), ifs, ipv4, ipv6, c, 30*time.Second)

	for result := range r {
		if result.Err != nil {
			log.Printf("Could not configure %s for %s: %v", result.Interface.Attrs().Name, result.Protocol, result.Err)
		} else if err := result.Lease.Configure(); err != nil {
			log.Printf("Could not configure %s for %s: %v", result.Interface.Attrs().Name, result.Protocol, err)
		} else {
			log.Printf("Configured %s with %s", result.Interface.Attrs().Name, result.Lease)
		}
	}
	log.Printf("Finished trying to configure all interfaces.")
}

// fetch an access token from the identity provider of the cluster.
// currently
func accessTokenFromRefresh(refreshToken string, identityProviderUrl string) (string, error) {
	u, err := url.Parse(identityProviderUrl)
	if err != nil {
		return "", err
	}

	urlValues := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {"cloud-services"},
		"refresh_token": {refreshToken},
	}

	form, err := httpClient.PostForm(u.String(), urlValues)
	if err != nil {
		return "", err
	}

	response := struct {
		AccessToken string `json:"access_token"`
	}{}
	all, err := io.ReadAll(form.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response %w", err)
	}

	err = json.Unmarshal(all, &response)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal the refresh token %w", err)
	}

	return response.AccessToken, nil
}
