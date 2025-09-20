package e2e

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = When("Running the built binary", Ordered, func() {
	BeforeEach(func() {
		By("Logging the current routes and rules prior to starting the test")
		logRoutingState()
	})

	AfterEach(func() {
		By("Logging the current routes and rules after finishing the test")
		logRoutingState()
	})

	It("should show the help message", func() {
		helpText, err := Run(exec.Command(binaryPath, "--help"))
		Expect(err).NotTo(HaveOccurred(), "Failed to run binary")
		Expect(helpText).To(ContainSubstring("Usage of"), "Help text should contain usage information")
	})

	It("should set the configured routes", func() {
		defer runBinaryUpdateRoutes("127.0.0.10", "127.0.0.10")()
		checkRoutes("127.0.0.10")
	})

	It("should set multiple nexthops", func() {
		defer runBinaryUpdateRoutes("127.0.0.10", "127.0.0.12")()
		checkRoutes("127.0.0.10", "127.0.0.12")
	})

	It("should remove the configured routes when no gateways are healthy", func() {
		defer runBinaryUpdateRoutes("127.0.0.11", "127.0.0.11")()
		checkRoutes()
	})

	It("should add another nexthop when a gateway becomes healthy", func() {
		defer runBinaryUpdateRoutes("127.0.0.10", "127.0.0.12")()

		// Check that the configured routes have been set to the two health gateways
		checkRoutes("127.0.0.10", "127.0.0.12")

		// Start another healthcheck web server on 127.0.0.11:8080
		StartTestWebServer("127.0.0.11:8080")

		// Check that the configured routes have been updated to include 127.0.0.11 nexthop
		checkRoutes("127.0.0.10", "127.0.0.11", "127.0.0.12")
	})

	It("should remove a nexthop when a gateway becomes unhealthy", func() {
		defer runBinaryUpdateRoutes("127.0.0.10", "127.0.0.12")()

		// Start another healthcheck web server on 127.0.0.11:8080
		stopServer := StartTestWebServer("127.0.0.11:8080")

		// Check that the configured routes includes 127.0.0.11 nexthop
		checkRoutes("127.0.0.10", "127.0.0.11", "127.0.0.12")

		stopServer()

		// Check that the configured routes have dropped the 127.0.0.11 nexthop
		checkRoutes("127.0.0.10", "127.0.0.12")
	})
})

func runBinaryUpdateRoutes(startIP, endIP string) func() {
	ddnsArgs := []string{}
	dynudnsAPIKey := os.Getenv("DYNUDNS_API_KEY")
	dynudnsHostname := os.Getenv("DYNUDNS_HOSTNAME")

	if dynudnsAPIKey != "" && dynudnsHostname != "" {
		// This will fail if the dynudns API endpoints ever resolve to 1/8 or 2/8 because the test uses those as route destinations.
		ddnsArgs = []string{
			"-ddns-provider", "dynudns",
			"-ddns-password", dynudnsAPIKey,
			"-ddns-hostname", dynudnsHostname,
			"-public-ip-service-scheme", "http",
			"-public-ip-service-port", "8080",
			"-public-ip-service-path", "/v1/ip",
		}
	}

	teardown := runBinary(
		append(
			ddnsArgs,
			"-check-period", "250ms",
			"-timeout", "100ms",
			"-path", "/healthz",
			"-port", "8080",
			"-start-ip", startIP,
			"-end-ip", endIP,
			"-exclude-cidr", "10.0.0.0/8",
			"-exclude-cidr", "172.16.0.0/12",
			"-exclude-cidr", "192.168.0.0/16",
			"-exclude-reserved-cidrs",
			"-log-level", "debug",
			"-route", "1.0.0.0/8",
			"-route", "2.0.0.0/8",
		)...,
	)

	// Sleep a moment to allow for DynuDNS provider initialization
	if dynudnsAPIKey != "" && dynudnsHostname != "" {
		time.Sleep(10 * time.Second)
	}

	return teardown
}

func checkRoutes(expectedNextHopsIPs ...string) {
	Eventually(func(g Gomega) {
		By(fmt.Sprintf("Checking for configured routes with next hops: %v", expectedNextHopsIPs))
		configuredRoutes := getConfiguredIPv4Routes(g)
		if len(expectedNextHopsIPs) == 0 {
			g.Expect(configuredRoutes).To(HaveLen(0), "There should be no configured routes")
			return
		}

		g.Expect(configuredRoutes).To(HaveLen(2), "There should be exactly two configured routes")
		for i, configuredRoute := range configuredRoutes {
			g.Expect(configuredRoute).To(HaveLen(len(expectedNextHopsIPs)), "Configured route %d should have the expected number of next hops", i)

			for j, expectedHop := range expectedNextHopsIPs {
				expectedHop := net.ParseIP(expectedHop)
				g.Expect(expectedHop).NotTo(BeNil(), "Expected hop should be a valid IP (test bug)")

				g.Expect(configuredRoute[j].Equal(expectedHop)).To(BeTrue(), "Next hop %d should be %s", j, expectedHop.String())
			}

			if len(expectedNextHopsIPs) == 0 {
				return
			}
		}

		By("Checking that excluded destination CIDRs get forwarded via the main routing table")
		testAddresses := []string{
			"10.1.2.3",
			"172.16.4.5",
			"192.168.6.7",
		}
		for _, addr := range testAddresses {
			output, err := Run(exec.Command("ip", "route", "get", addr))
			g.Expect(err).NotTo(HaveOccurred(), "Failed to get route for excluded CIDR address %s: %s", addr, output)
			g.Expect(output).To(ContainSubstring("src %s", testNetNSAddress), "Excluded CIDR address %s should be routed via main table", addr)
		}

		By("Checking that non-excluded destination CIDRs get forwarded via the gateway table")
		testAddress := "1.2.3.4"
		output, err := Run(exec.Command("ip", "route", "get", testAddress))
		g.Expect(err).NotTo(HaveOccurred(), "Failed to get route for non-excluded CIDR address %s: %s", testAddress, output)
		g.Expect(output).To(ContainSubstring("table 180"), "Non-excluded CIDR address %s should be routed via gateway table", testAddress)
	}, 2*time.Second, 250*time.Millisecond).Should(Succeed(), "Failed to verify configured routes")
}

// runBinary starts the built binary with the given arguments and returns a function to stop it
func runBinary(args ...string) func() {
	ctx, cancel := context.WithCancel(GinkgoT().Context())

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Stdout = GinkgoWriter
	cmd.Stderr = GinkgoWriter
	dir, err := GetProjectDir()
	Expect(err).NotTo(HaveOccurred(), "Failed to get project dir")
	cmd.Dir = dir

	err = cmd.Start()
	Expect(err).NotTo(HaveOccurred(), "Failed to start binary")

	return func() {
		cmd.Process.Signal(os.Interrupt)
		time.Sleep(500 * time.Millisecond) // Give it some time to shutdown

		// Ensure the process has exited
		cancel()
		cmd.Wait()
	}
}

func logRoutingState() {
	// Show all routes
	output, err := Run(exec.Command("ip", "-o", "-4", "route", "show", "table", "all"))
	Expect(err).NotTo(HaveOccurred(), "Failed to get IPv4 routes")
	fmt.Fprintf(GinkgoWriter, "All IPv4 routes:\n%s\n", output)

	// Show all rules
	output, err = Run(exec.Command("ip", "rule", "list"))
	Expect(err).NotTo(HaveOccurred(), "Failed to get IP rules")
	fmt.Fprintf(GinkgoWriter, "All IP rules:\n%s\n", output)
}

// getConfiguredIPv4Routes returns the current configured IPv4 routes in the test namespace that are
// assigned to the gateway table
func getConfiguredIPv4Routes(g Gomega) [][]net.IP {
	return append(
		getIPv4Routes(g, "1.0.0.0/8"),
		getIPv4Routes(g, "2.0.0.0/8")...,
	)
}

func getIPv4Routes(g Gomega, destination string) [][]net.IP {
	output, err := Run(exec.Command("ip", "-o", "-4", "route", "show", destination, "table", "180"))
	g.Expect(err).NotTo(HaveOccurred(), "Failed to get IPv4 routes to %s", destination)

	lines := GetNonEmptyLines(output)

	regex, err := regexp.Compile(`via ([0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3})`)
	g.Expect(err).NotTo(HaveOccurred(), "Failed to compile regex")

	destinationRoutes := make([][]net.IP, 0, len(lines))
	for _, line := range lines {
		matches := regex.FindAllStringSubmatch(line, 100)

		hops := make([]net.IP, 0, len(matches))
		for _, match := range matches {
			for _, ip := range match[1:] {
				nextHop := net.ParseIP(ip)
				g.Expect(nextHop).NotTo(BeNil(), "Failed to parse IP from route output")

				hops = append(hops, nextHop)
			}
		}
		destinationRoutes = append(destinationRoutes, hops)
	}

	return destinationRoutes
}
