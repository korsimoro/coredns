package kubernetes

import (
	"net"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coredns/coredns/middleware/kubernetes/autopath"
	"github.com/coredns/coredns/middleware/test"

	"github.com/mholt/caddy"
	"github.com/miekg/dns"
	unversionedapi "k8s.io/client-go/1.5/pkg/api/unversioned"
)

func parseCidr(cidr string) net.IPNet {
	_, ipnet, _ := net.ParseCIDR(cidr)
	return *ipnet
}

func TestKubernetesParse(t *testing.T) {
	f, rm, err := test.TempFile(os.TempDir(), testResolveConf)
	autoPathResolvConfFile := f
	if err != nil {
		t.Fatalf("Could not create resolv.conf TempFile: %s", err)
	}
	defer rm()

	tests := []struct {
		description           string        // Human-facing description of test case
		input                 string        // Corefile data as string
		shouldErr             bool          // true if test case is exected to produce an error.
		expectedErrContent    string        // substring from the expected error. Empty for positive cases.
		expectedZoneCount     int           // expected count of defined zones.
		expectedNSCount       int           // expected count of namespaces.
		expectedResyncPeriod  time.Duration // expected resync period value
		expectedLabelSelector string        // expected label selector value
		expectedPodMode       string
		expectedCidrs         []net.IPNet
		expectedFallthrough   bool
		expectedUpstreams     []string
		expectedFederations   []Federation
		expectedAutoPath      *autopath.AutoPath
	}{
		// positive
		{
			"kubernetes keyword with one zone",
			`kubernetes coredns.local`,
			false,
			"",
			1,
			0,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		{
			"kubernetes keyword with multiple zones",
			`kubernetes coredns.local test.local`,
			false,
			"",
			2,
			0,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		{
			"kubernetes keyword with zone and empty braces",
			`kubernetes coredns.local {
}`,
			false,
			"",
			1,
			0,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		{
			"endpoint keyword with url",
			`kubernetes coredns.local {
	endpoint http://localhost:9090
}`,
			false,
			"",
			1,
			0,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		{
			"namespaces keyword with one namespace",
			`kubernetes coredns.local {
	namespaces demo
}`,
			false,
			"",
			1,
			1,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			nil,
			nil,
		},
		{
			"namespaces keyword with multiple namespaces",
			`kubernetes coredns.local {
	namespaces demo test
}`,
			false,
			"",
			1,
			2,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		{
			"resync period in seconds",
			`kubernetes coredns.local {
    resyncperiod 30s
}`,
			false,
			"",
			1,
			0,
			30 * time.Second,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		{
			"resync period in minutes",
			`kubernetes coredns.local {
    resyncperiod 15m
}`,
			false,
			"",
			1,
			0,
			15 * time.Minute,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		{
			"basic label selector",
			`kubernetes coredns.local {
    labels environment=prod
}`,
			false,
			"",
			1,
			0,
			defaultResyncPeriod,
			"environment=prod",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		{
			"multi-label selector",
			`kubernetes coredns.local {
    labels environment in (production, staging, qa),application=nginx
}`,
			false,
			"",
			1,
			0,
			defaultResyncPeriod,
			"application=nginx,environment in (production,qa,staging)",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		{
			"fully specified valid config",
			`kubernetes coredns.local test.local {
    resyncperiod 15m
	endpoint http://localhost:8080
	namespaces demo test
    labels environment in (production, staging, qa),application=nginx
    fallthrough
}`,
			false,
			"",
			2,
			2,
			15 * time.Minute,
			"application=nginx,environment in (production,qa,staging)",
			PodModeDisabled,
			nil,
			true,
			nil,
			[]Federation{},
			nil,
		},
		// negative
		{
			"no kubernetes keyword",
			"",
			true,
			"kubernetes setup called without keyword 'kubernetes' in Corefile",
			-1,
			-1,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		{
			"kubernetes keyword without a zone",
			`kubernetes`,
			true,
			"zone name must be provided for kubernetes middleware",
			-1,
			0,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		{
			"endpoint keyword without an endpoint value",
			`kubernetes coredns.local {
    endpoint
}`,
			true,
			"rong argument count or unexpected line ending",
			-1,
			-1,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		{
			"namespace keyword without a namespace value",
			`kubernetes coredns.local {
	namespaces
}`,
			true,
			"rong argument count or unexpected line ending",
			-1,
			-1,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		{
			"resyncperiod keyword without a duration value",
			`kubernetes coredns.local {
    resyncperiod
}`,
			true,
			"rong argument count or unexpected line ending",
			-1,
			0,
			0 * time.Minute,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		{
			"resync period no units",
			`kubernetes coredns.local {
    resyncperiod 15
}`,
			true,
			"unable to parse resync duration value",
			-1,
			0,
			0 * time.Second,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		{
			"resync period invalid",
			`kubernetes coredns.local {
    resyncperiod abc
}`,
			true,
			"unable to parse resync duration value",
			-1,
			0,
			0 * time.Second,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		{
			"labels with no selector value",
			`kubernetes coredns.local {
    labels
}`,
			true,
			"rong argument count or unexpected line ending",
			-1,
			0,
			0 * time.Second,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		{
			"labels with invalid selector value",
			`kubernetes coredns.local {
    labels environment in (production, qa
}`,
			true,
			"unable to parse label selector",
			-1,
			0,
			0 * time.Second,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		// pods disabled
		{
			"pods disabled",
			`kubernetes coredns.local {
	pods disabled
}`,
			false,
			"",
			1,
			0,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		// pods insecure
		{
			"pods insecure",
			`kubernetes coredns.local {
	pods insecure
}`,
			false,
			"",
			1,
			0,
			defaultResyncPeriod,
			"",
			PodModeInsecure,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		// pods verified
		{
			"pods verified",
			`kubernetes coredns.local {
	pods verified
}`,
			false,
			"",
			1,
			0,
			defaultResyncPeriod,
			"",
			PodModeVerified,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		// pods invalid
		{
			"invalid pods mode",
			`kubernetes coredns.local {
	pods giant_seed
}`,
			true,
			"rong value for pods",
			-1,
			0,
			defaultResyncPeriod,
			"",
			PodModeVerified,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		// cidrs ok
		{
			"valid cidrs",
			`kubernetes coredns.local {
	cidrs 10.0.0.0/24 10.0.1.0/24
}`,
			false,
			"",
			1,
			0,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			[]net.IPNet{parseCidr("10.0.0.0/24"), parseCidr("10.0.1.0/24")},
			false,
			nil,
			[]Federation{},
			nil,
		},
		// cidrs ok
		{
			"invalid cidr: hard",
			`kubernetes coredns.local {
	cidrs hard dry
}`,
			true,
			"invalid cidr: hard",
			-1,
			0,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		// fallthrough invalid
		{
			"Extra params for fallthrough",
			`kubernetes coredns.local {
	fallthrough junk
}`,
			true,
			"rong argument count",
			-1,
			0,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		// Valid upstream
		{
			"valid upstream",
			`kubernetes coredns.local {
	upstream 13.14.15.16:53
}`,
			false,
			"",
			1,
			0,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			[]string{"13.14.15.16:53"},
			[]Federation{},
			nil,
		},
		// Invalid upstream
		{
			"valid upstream",
			`kubernetes coredns.local {
	upstream 13.14.15.16orange
}`,
			true,
			"not an IP address or file: \"13.14.15.16orange\"",
			-1,
			0,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		// Valid federations
		{
			"valid upstream",
			`kubernetes coredns.local {
	federation foo bar.crawl.com
	federation fed era.tion.com
}`,
			false,
			"",
			1,
			0,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{
				{name: "foo", zone: "bar.crawl.com"},
				{name: "fed", zone: "era.tion.com"},
			},
			nil,
		},
		// Invalid federations
		{
			"valid upstream",
			`kubernetes coredns.local {
	federation starship
}`,
			true,
			`incorrect number of arguments for federation`,
			-1,
			0,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			[]Federation{},
			nil,
		},
		// autopath
		{
			"valid autopath",
			`kubernetes coredns.local {
	autopath 1 NXDOMAIN ` + autoPathResolvConfFile + `
}`,
			false,
			"",
			1,
			0,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			nil,
			&autopath.AutoPath{
				NDots:          1,
				HostSearchPath: []string{"bar.com.", "baz.com."},
				ResolvConfFile: autoPathResolvConfFile,
				OnNXDOMAIN:     dns.RcodeNameError,
			},
		},
		{
			"invalid autopath RESPONSE",
			`kubernetes coredns.local {
	autopath 0 CRY
}`,
			true,
			"invalid RESPONSE argument for autopath",
			-1,
			0,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			nil,
			nil,
		},
		{
			"invalid autopath NDOTS",
			`kubernetes coredns.local {
	autopath polka
}`,
			true,
			"invalid NDOTS argument for autopath",
			-1,
			0,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			nil,
			nil,
		},
		{
			"invalid autopath RESOLV-CONF",
			`kubernetes coredns.local {
	autopath 1 NOERROR /wrong/path/to/resolv.conf
}`,
			true,
			"error when parsing",
			-1,
			0,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			nil,
			nil,
		},
		{
			"invalid autopath invalid option",
			`kubernetes coredns.local {
	autopath 1 SERVFAIL ` + autoPathResolvConfFile + ` foo
}`,
			true,
			"incorrect number of arguments",
			-1,
			0,
			defaultResyncPeriod,
			"",
			PodModeDisabled,
			nil,
			false,
			nil,
			nil,
			nil,
		},
	}

	for i, test := range tests {
		c := caddy.NewTestController("dns", test.input)
		k8sController, err := kubernetesParse(c)

		if test.shouldErr && err == nil {
			t.Errorf("Test %d: Expected error, but did not find error for input '%s'. Error was: '%v'", i, test.input, err)
		}

		if err != nil {
			if !test.shouldErr {
				t.Errorf("Test %d: Expected no error but found one for input %s. Error was: %v", i, test.input, err)
				continue
			}

			if test.shouldErr && (len(test.expectedErrContent) < 1) {
				t.Fatalf("Test %d: Test marked as expecting an error, but no expectedErrContent provided for input '%s'. Error was: '%v'", i, test.input, err)
			}

			if test.shouldErr && (test.expectedZoneCount >= 0) {
				t.Errorf("Test %d: Test marked as expecting an error, but provides value for expectedZoneCount!=-1 for input '%s'. Error was: '%v'", i, test.input, err)
			}

			if !strings.Contains(err.Error(), test.expectedErrContent) {
				t.Errorf("Test %d: Expected error to contain: %v, found error: %v, input: %s", i, test.expectedErrContent, err, test.input)
			}
			continue
		}

		// No error was raised, so validate initialization of k8sController
		//     Zones
		foundZoneCount := len(k8sController.Zones)
		if foundZoneCount != test.expectedZoneCount {
			t.Errorf("Test %d: Expected kubernetes controller to be initialized with %d zones, instead found %d zones: '%v' for input '%s'", i, test.expectedZoneCount, foundZoneCount, k8sController.Zones, test.input)
		}

		//    Namespaces
		foundNSCount := len(k8sController.Namespaces)
		if foundNSCount != test.expectedNSCount {
			t.Errorf("Test %d: Expected kubernetes controller to be initialized with %d namespaces. Instead found %d namespaces: '%v' for input '%s'", i, test.expectedNSCount, foundNSCount, k8sController.Namespaces, test.input)
		}

		//    ResyncPeriod
		foundResyncPeriod := k8sController.ResyncPeriod
		if foundResyncPeriod != test.expectedResyncPeriod {
			t.Errorf("Test %d: Expected kubernetes controller to be initialized with resync period '%s'. Instead found period '%s' for input '%s'", i, test.expectedResyncPeriod, foundResyncPeriod, test.input)
		}

		//    Labels
		if k8sController.LabelSelector != nil {
			foundLabelSelectorString := unversionedapi.FormatLabelSelector(k8sController.LabelSelector)
			if foundLabelSelectorString != test.expectedLabelSelector {
				t.Errorf("Test %d: Expected kubernetes controller to be initialized with label selector '%s'. Instead found selector '%s' for input '%s'", i, test.expectedLabelSelector, foundLabelSelectorString, test.input)
			}
		}
		//    Pods
		foundPodMode := k8sController.PodMode
		if foundPodMode != test.expectedPodMode {
			t.Errorf("Test %d: Expected kubernetes controller to be initialized with pod mode '%s'. Instead found pod mode '%s' for input '%s'", i, test.expectedPodMode, foundPodMode, test.input)
		}

		//    Cidrs
		foundCidrs := k8sController.ReverseCidrs
		if len(foundCidrs) != len(test.expectedCidrs) {
			t.Errorf("Test %d: Expected kubernetes controller to be initialized with %d cidrs. Instead found %d cidrs for input '%s'", i, len(test.expectedCidrs), len(foundCidrs), test.input)
		}
		for j, cidr := range test.expectedCidrs {
			if cidr.String() != foundCidrs[j].String() {
				t.Errorf("Test %d: Expected kubernetes controller to be initialized with cidr '%s'. Instead found cidr '%s' for input '%s'", i, test.expectedCidrs[j].String(), foundCidrs[j].String(), test.input)
			}
		}
		// fallthrough
		foundFallthrough := k8sController.Fallthrough
		if foundFallthrough != test.expectedFallthrough {
			t.Errorf("Test %d: Expected kubernetes controller to be initialized with fallthrough '%v'. Instead found fallthrough '%v' for input '%s'", i, test.expectedFallthrough, foundFallthrough, test.input)
		}
		// upstream
		foundUpstreams := k8sController.Proxy.Upstreams
		if test.expectedUpstreams == nil {
			if foundUpstreams != nil {
				t.Errorf("Test %d: Expected kubernetes controller to not be initialized with upstreams for input '%s'", i, test.input)
			}
		} else {
			if foundUpstreams == nil {
				t.Errorf("Test %d: Expected kubernetes controller to be initialized with upstreams for input '%s'", i, test.input)
			} else {
				if len(*foundUpstreams) != len(test.expectedUpstreams) {
					t.Errorf("Test %d: Expected kubernetes controller to be initialized with %d upstreams. Instead found %d upstreams for input '%s'", i, len(test.expectedUpstreams), len(*foundUpstreams), test.input)
				}
				for j, want := range test.expectedUpstreams {
					got := (*foundUpstreams)[j].Select().Name
					if got != want {
						t.Errorf("Test %d: Expected kubernetes controller to be initialized with upstream '%s'. Instead found upstream '%s' for input '%s'", i, want, got, test.input)
					}
				}

			}
		}
		// autopath
		if !reflect.DeepEqual(test.expectedAutoPath, k8sController.autoPath) {
			t.Errorf("Test %d: Expected kubernetes controller to be initialized with autopath '%v'. Instead found autopath '%v' for input '%s'", i, test.expectedAutoPath, k8sController.autoPath, test.input)
		}
	}
}

const testResolveConf = `nameserver 1.2.3.4
domain foo.com
search bar.com baz.com
options ndots:5
`
