package e2e

import (
	"context"
	"net"
	"os/exec"
	"regexp"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = When("Running the built binary", func() {
	It("should show the help message", func() {
		helpText, err := Run(exec.Command(binaryPath, "--help"))
		Expect(err).NotTo(HaveOccurred(), "Failed to run binary")
		Expect(helpText).To(ContainSubstring("Usage of"), "Help text should contain usage information")
	})

	It("should set the default route", func() {
		runBinaryUpdateRoutes("127.0.0.10", "127.0.0.10")
		checkRoutes("127.0.0.10")
	})

	It("should set multiple default routes", func() {
		runBinaryUpdateRoutes("127.0.0.10", "127.0.0.12")
		checkRoutes("127.0.0.10", "127.0.0.12")
	})

	It("should remove the default route when no gateways are healthy", func() {
		runBinaryUpdateRoutes("127.0.0.11", "127.0.0.11")
		checkRoutes()
	})

	It("should set the default route when there is no gateawy", func() {
		By("removing any existing default routes")
		output, err := Run(exec.Command("ip", "route", "del", "default"))
		Expect(err).To(Or(BeNil(), MatchError(ContainSubstring("No such process"))), "Failed to delete default route: %s", output)

		By("starting the binary to add a default route")
		runBinaryUpdateRoutes("127.0.0.10", "127.0.0.10")
		checkRoutes("127.0.0.10")
	})

	It("should add another nexthop when a gateway becomes healthy", func() {
		runBinaryUpdateRoutes("127.0.0.10", "127.0.0.12")

		// Check that the default route has been set to the two health gateways
		checkRoutes("127.0.0.10", "127.0.0.12")

		// Start another healthcheck web server on 127.0.0.11:8080
		StartTestWebServer("127.0.0.11:8080")

		// Check that the default route has been updated to include 127.0.0.11
		checkRoutes("127.0.0.10", "127.0.0.11", "127.0.0.12")
	})

	It("should remove a nexthop when a gateway becomes unhealthy", func() {
		runBinaryUpdateRoutes("127.0.0.10", "127.0.0.12")

		// Start another healthcheck web server on 127.0.0.11:8080
		stopServer := StartTestWebServer("127.0.0.11:8080")

		// Check that the default route includes 127.0.0.11
		checkRoutes("127.0.0.10", "127.0.0.11", "127.0.0.12")

		stopServer()

		// Check that the default route has dropped the 127.0.0.11 nexthop
		checkRoutes("127.0.0.10", "127.0.0.12")
	})
})

func runBinaryUpdateRoutes(startIP, endIP string) {
	runBinary(
		"-check-period", "250ms",
		"-timeout", "100ms",
		"-path", "/healthz",
		"-port", "8080",
		"-start-ip", startIP,
		"-end-ip", endIP,
		"-verbose",
	)
}

func checkRoutes(expectedNextHopsIPs ...string) {
	Eventually(func(g Gomega) {
		defaultRoutes := getDefaultIPv4Routes()
		if len(expectedNextHopsIPs) == 0 {
			g.Expect(defaultRoutes).To(HaveLen(0), "There should be no default routes")
			return
		}

		g.Expect(defaultRoutes).To(HaveLen(1), "There should be exactly one default route")
		defaultRoute := defaultRoutes[0]
		g.Expect(defaultRoute).To(HaveLen(len(expectedNextHopsIPs)), "Default route should have the expected number of next hops")

		for i, expectedHop := range expectedNextHopsIPs {
			expectedHop := net.ParseIP(expectedHop)
			g.Expect(expectedHop).NotTo(BeNil(), "Expected hop should be a valid IP (test bug)")

			g.Expect(defaultRoute[i].Equal(expectedHop)).To(BeTrue(), "Next hop %d should be %s", i, expectedHop.String())
		}
	}, 2*time.Second, 250*time.Millisecond).Should(Succeed(), "Failed to verify default route")
}

// runBinary starts the built binary with the given arguments and returns a function to stop it
func runBinary(args ...string) {
	ctx, cancel := context.WithCancel(GinkgoT().Context())

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Stdout = GinkgoWriter
	cmd.Stderr = GinkgoWriter
	dir, err := GetProjectDir()
	Expect(err).NotTo(HaveOccurred(), "Failed to get project dir")
	cmd.Dir = dir

	err = cmd.Start()
	Expect(err).NotTo(HaveOccurred(), "Failed to start binary")

	DeferCleanup(func() {
		cancel()
		cmd.Wait()
	})
}

func getDefaultIPv4Routes() [][]net.IP {
	output, err := Run(exec.Command("ip", "-o", "-4", "route", "show", "default"))
	Expect(err).NotTo(HaveOccurred(), "Failed to get default IPv4 routes")

	lines := GetNonEmptyLines(output)

	regex, err := regexp.Compile(`via ([0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3})`)
	Expect(err).NotTo(HaveOccurred(), "Failed to compile regex")

	defaultRoutes := make([][]net.IP, 0, len(lines))
	for _, line := range lines {
		matches := regex.FindAllStringSubmatch(line, 100)

		hops := make([]net.IP, 0, len(matches))
		for _, match := range matches {
			for _, ip := range match[1:] {
				nextHop := net.ParseIP(ip)
				if nextHop == nil {
					Fail("Failed to parse IP from route output")
				}

				hops = append(hops, nextHop)
			}
		}
		defaultRoutes = append(defaultRoutes, hops)
	}

	return defaultRoutes
}
