package main

import (
    "context"
    "flag"
    "fmt"
    "github.com/insomniacslk/dhcp/dhcpv4"
    "github.com/u-root/u-root/pkg/boot"
    "github.com/u-root/u-root/pkg/boot/bootcmd"
    "github.com/u-root/u-root/pkg/boot/menu"
    "github.com/u-root/u-root/pkg/boot/netboot"
    "github.com/u-root/u-root/pkg/curl"
    "github.com/u-root/u-root/pkg/dhclient"
    "github.com/u-root/u-root/pkg/ulog"
    "github.com/vishvananda/netlink"
    "log"
    "net"
    "os"
    "strings"
    "time"
)

var (
    apiURL         = flag.String("api-url", "https://api.openshift.com/api", "The url of the api-server")
    tokenFile      = flag.String("tokenFile", "", "A file containing he bearer tokenFile authorizing the api calls")
    infraEnvIDFile = flag.String("infraEnvIDFile", "", "A file containing the ID of the infraenv object")
)

func main() {
    flag.Parse()
    if *tokenFile == "" || *infraEnvIDFile == "" {
        log.Fatalf("tokenFile or infraEnvIDFile are empty. Pass -tokenFile /file and -inrfaEnvID /file")
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
    data, err = os.ReadFile(*tokenFile)
    if err != nil {
        log.Fatalf("failed reading the token file %s: %v", tokenFile, err)
    }
    token := strings.TrimSuffix(string(data), "\n")
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
