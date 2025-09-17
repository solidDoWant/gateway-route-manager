package e2e

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	binaryPath       string
	testNetNSHandle  *netns.NsHandle
	testNetNSCIDR    = "192.168.50.0/31" // Two addresses, one for each side of the veth pair
	testNetNSAddress string
)

func TestManager(t *testing.T) {
	require.NoError(t, BuildBinary(), "Failed to build binary")

	RegisterFailHandler(Fail)
	RunSpecs(t, "Manager Suite", AroundNode(withTestNetworkNamespace))
}

var _ = BeforeSuite(func() {
	StartTestWebServer("127.0.0.10:8080")
	StartTestWebServer("127.0.0.12:8080")

	var err error
	binaryPath, err = GetBinaryPath()
	Expect(err).NotTo(HaveOccurred(), "Failed to get binary path")
})

// This needs to be called outside of any Ginkgo functions to ensure it runs
// in the host namespace (needed for deps)
func BuildBinary() error {
	buildCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(buildCtx, "make", "binary")

	dir, err := GetProjectDir()
	if err != nil {
		return fmt.Errorf("failed to get project dir: %w", err)
	}

	cmd.Dir = dir

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build binary: %w", err)
	}

	return nil
}

func StartTestWebServer(address string) func() {
	By("Starting a webserver on the host for a health check")

	mux := http.NewServeMux()
	mux.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "200 OK")
	}))
	mux.Handle("/v1/ip", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"public_ip": "%s"}`, testNetNSAddress)
	}))
	server := &http.Server{
		Addr:    address,
		Handler: mux,
	}

	go withTestNetworkNamespace(context.TODO(), func(ctx context.Context) {
		Expect(server.ListenAndServe()).Should(Or(Succeed(), MatchError(http.ErrServerClosed)))
	})

	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)

	go func() {
		<-ctx.Done()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		Expect(server.Shutdown(shutdownCtx)).NotTo(HaveOccurred(), "Failed to shutdown test web server")
	}()

	// Give the server a moment to start
	Eventually(func() error {
		// http.Get uses another goroutine, so it doesn't inherit the network namespace.
		// Use curl instead because it "just works".
		ctx, cancel := context.WithTimeout(GinkgoT().Context(), 40*time.Millisecond)
		defer cancel()

		_, err := Run(exec.CommandContext(ctx, "curl", "-fsSLv", fmt.Sprintf("http://%s/healthz", address)))
		return err
	}, 2*time.Second, 50*time.Millisecond).Should(Succeed(), "Test web server should be reachable")

	return cancel
}

func logNetNS(prefix string) {
	netNSPath := filepath.Join("/proc", fmt.Sprintf("%d", os.Getpid()), "task", fmt.Sprintf("%d", unix.Gettid()), "ns", "net")
	target, err := os.Readlink(netNSPath)
	Expect(err).NotTo(HaveOccurred(), "Failed to read current network namespace link %q", netNSPath)
	fmt.Printf("%s network namespace link: %s -> %s\n", prefix, netNSPath, target)
}

// withTestNetworkNamespace wraps a function to run in a specific network namespace.
func withTestNetworkNamespace(ctx context.Context, f func(context.Context)) {
	// Important: namespaces are per "thread", which are basically just Linux processes. The current
	// execution context must be locked to a thread so that all statements run in the same namespace.
	// The thread is never unlocked, causing it to be thrown away when the goroutine completes. This
	// ensures that the caller's network namespace is not changed by the function, even when the
	// called function errors.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Setup the test network namespace if it doesn't exist
	if testNetNSHandle == nil || !testNetNSHandle.IsOpen() {
		threadUnsafeSetupTestNetworkNamespace()
	}

	// Switch to the test network namespace
	Expect(netns.Set(*testNetNSHandle)).To(Succeed(), "Failed to set thread's network namespace")

	GinkgoHelper()
	f(ctx)
}

// threadUnsafeSetupTestNetworkNamespace sets up a test network namespace. It should be executed exclusively
// from a single thread that starts in the host network namespace.
func threadUnsafeSetupTestNetworkNamespace() {
	// Create a veth pair to allow communication from the host to the test network namespace
	// This is needed for some of the setup logic (etcd, etc.) to work correctly.
	vethAttrs := netlink.NewLinkAttrs()
	vethAttrs.Name = "test.veth0"
	vethLink := netlink.NewVeth(vethAttrs)
	vethLink.PeerName = "test.veth1"

	// Delete existing interfaces if they exist
	// Either they both exists, or none of them exists, so only one deletion is needed.
	link, err := netlink.LinkByName(vethAttrs.Name)
	Expect(err).To(Or(Succeed(), BeAssignableToTypeOf(netlink.LinkNotFoundError{})), "Failed to get link %q by name", vethAttrs.Name)

	if link != nil {
		Expect(netlink.LinkDel(link)).To(Succeed(), "Failed to delete existing link %q", vethAttrs.Name)
	}

	// Create the veth pair in the host namespace
	Expect(netlink.LinkAdd(vethLink)).To(Succeed(), "Failed to create veth pair")

	// Configure the host namespace interface
	hostNamespaceNet, err := netlink.ParseIPNet(testNetNSCIDR)
	Expect(err).NotTo(HaveOccurred(), "Failed to parse CIDR address %q", testNetNSCIDR)

	seutpInterface := func(interfaceName string, net *net.IPNet) {
		link, err := netlink.LinkByName(interfaceName)
		Expect(err).NotTo(HaveOccurred(), "Failed to get link by name %q", interfaceName)

		// Set the link up
		Expect(netlink.LinkSetUp(link)).To(Succeed(), "Failed to set link %q up", interfaceName)

		// Add the IP address to the link
		Expect(netlink.AddrAdd(link, &netlink.Addr{IPNet: net})).To(Succeed(),
			"Failed to add address %q to link %q", net, interfaceName)
	}
	seutpInterface(vethLink.Name, hostNamespaceNet)

	// Create the new network namespace
	currentNamespace, err := netns.Get()
	Expect(err).NotTo(HaveOccurred(), "Failed to get current network namespace")

	netnsName := "manager-testing"
	// This can happen if a previous test run did not clean up properly (panic, failure, etc.)
	if checkHandle, err := netns.GetFromName(netnsName); err == nil {
		Expect(checkHandle.Close()).To(Succeed(), "Failed to close existing network namespace handle")
		Expect(netns.DeleteNamed(netnsName)).To(Succeed(), "Failed to delete existing network namespace")
	}

	newHandle, err := netns.NewNamed(netnsName)
	Expect(err).NotTo(HaveOccurred(), "Failed to create new network namespace")

	testNetNSHandle = &newHandle
	DeferCleanup(func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		Expect(netns.Set(currentNamespace)).To(Succeed(), "Failed to set thread's network namespace back to host")
		Expect(currentNamespace.Close()).To(Succeed(), "Failed to close current network namespace")
		Expect(netns.DeleteNamed(netnsName)).To(Succeed(), "Failed to delete test network namespace")
		Expect(testNetNSHandle.Close()).To(Succeed(), "Failed to close test network namespace handle")
		testNetNSHandle = nil
	})

	// Switch back to the host network namespace and move the veth interface to the new namespace
	Expect(netns.Set(currentNamespace)).To(Succeed(), "Failed to set thread's network namespace back to host")

	// Move the peer interface to the new network namespace
	peerLink, err := netlink.LinkByName(vethLink.PeerName)
	Expect(err).NotTo(HaveOccurred(), "Failed to get peer link by name %q", vethLink.PeerName)
	Expect(netlink.LinkSetNsFd(peerLink, int(newHandle))).To(Succeed(),
		"Failed to move peer link to new network namespace %q", vethLink.PeerName)

	// Set up the peer interface in the new network namespace
	Expect(netns.Set(*testNetNSHandle)).To(Succeed(), "Failed to set thread's network namespace to test network namespace")
	testNamespaceNet := &net.IPNet{
		IP:   hostNamespaceNet.IP,
		Mask: hostNamespaceNet.Mask,
	}
	testNamespaceNet.IP[len(testNamespaceNet.IP)-1] += 1 // Increment the last byte for the peer address
	seutpInterface(vethLink.PeerName, testNamespaceNet)
	testNetNSAddress = testNamespaceNet.IP.String()

	// Ensure the loopback interface is up in the test network namespace
	links, err := netlink.LinkList()
	Expect(err).NotTo(HaveOccurred(), "Failed to list links in test network namespace")

	for _, link := range links {
		linkAttrs := link.Attrs()
		if linkAttrs == nil {
			continue
		}

		if linkAttrs.Flags&net.FlagLoopback != 0 {
			Expect(netlink.LinkSetUp(link)).To(Succeed(), "Failed to set loopback interface %q up", linkAttrs.Name)
			break
		}
	}

	// Add a dummy default route
	_, defaultDest, err := net.ParseCIDR("0.0.0.0/0")
	Expect(err).NotTo(HaveOccurred(), "Failed to parse default route CIDR")

	route := &netlink.Route{
		Dst: defaultDest,
		Gw:  net.ParseIP("127.0.0.128"),
		Src: net.ParseIP("127.0.0.129"),
	}
	Expect(netlink.RouteAdd(route)).To(Succeed(), "Failed to add dummy default route in test network namespace")

	// Deferred cleanup of the veth peers is not needed, because one of the two will be deleted
	// when the network namespace is deleted. Because veth interfaces are exclusively pairs,
	// deleting one side of the pair will automatically delete the other side.
}
