/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package route53

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"

	"k8s.io/kops/dnsprovider/pkg/dnsprovider"
	route53testing "k8s.io/kops/dnsprovider/pkg/dnsprovider/providers/aws/route53/stubs"
	"k8s.io/kops/dnsprovider/pkg/dnsprovider/rrstype"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"k8s.io/kops/dnsprovider/pkg/dnsprovider/tests"
)

func newTestInterface() (dnsprovider.Interface, error) {
	// Use this to test the real cloud service.
	// return dnsprovider.GetDnsProvider(ProviderName, strings.NewReader("\n[global]\nproject-id = federation0-cluster00"))
	return newFakeInterface() // Use this to stub out the entire cloud service
}

func newFakeInterface() (dnsprovider.Interface, error) {
	service := route53testing.NewRoute53APIStub()
	iface := New(service)
	// Add a fake zone to test against.
	params := &route53.CreateHostedZoneInput{
		CallerReference: aws.String("Nonce"),       // Required
		Name:            aws.String("example.com"), // Required
	}
	_, err := iface.service.CreateHostedZone(context.TODO(), params)
	if err != nil {
		return nil, err
	}
	return iface, nil
}

var interface_ dnsprovider.Interface

func TestMain(m *testing.M) {
	fmt.Printf("Parsing flags.\n")
	flag.Parse()
	var err error
	fmt.Printf("Getting new test interface.\n")
	interface_, err = newTestInterface()
	if err != nil {
		fmt.Printf("Error creating interface: %v", err)
		os.Exit(1)
	}
	fmt.Printf("Running tests...\n")
	os.Exit(m.Run())
}

// zones returns the zones interface for the configured dns provider account/project,
// or fails if it can't be found
func zones(t *testing.T) dnsprovider.Zones {
	zonesInterface, supported := interface_.Zones()
	if !supported {
		t.Fatalf("Zones interface not supported by interface %v", interface_)
	} else {
		t.Logf("Got zones %v\n", zonesInterface)
	}
	return zonesInterface
}

// firstZone returns the first zone for the configured dns provider account/project,
// or fails if it can't be found
func firstZone(t *testing.T) dnsprovider.Zone {
	t.Logf("Getting zones")
	z := zones(t)
	zones, err := z.List()
	if err != nil {
		t.Fatalf("Failed to list zones: %v", err)
	} else {
		t.Logf("Got zone list: %v\n", zones)
	}
	if len(zones) < 1 {
		t.Fatalf("Zone listing returned %d, expected >= %d", len(zones), 1)
	} else {
		t.Logf("Got at least 1 zone in list:%v\n", zones[0])
	}
	return zones[0]
}

/* rrs returns the ResourceRecordSets interface for a given zone */
func rrs(t *testing.T, zone dnsprovider.Zone) (r dnsprovider.ResourceRecordSets) {
	rrsets, supported := zone.ResourceRecordSets()
	if !supported {
		t.Fatalf("ResourceRecordSets interface not supported by zone %v", zone)
		return r
	}
	return rrsets
}

func listRrsOrFail(t *testing.T, rrsets dnsprovider.ResourceRecordSets) []dnsprovider.ResourceRecordSet {
	rrset, err := rrsets.List()
	if err != nil {
		t.Fatalf("Failed to list recordsets: %v", err)
	} else {
		t.Logf("Got %d recordsets: %v", len(rrset), rrset)
	}
	return rrset
}

func getExampleRrs(zone dnsprovider.Zone) dnsprovider.ResourceRecordSet {
	rrsets, _ := zone.ResourceRecordSets()
	return rrsets.New("www11."+zone.Name(), []string{"10.10.10.10", "169.20.20.20"}, 180, rrstype.A)
}

func addRrsetOrFail(ctx context.Context, t *testing.T, rrsets dnsprovider.ResourceRecordSets, rrset dnsprovider.ResourceRecordSet) {
	err := rrsets.StartChangeset().Add(rrset).Apply(ctx)
	if err != nil {
		t.Fatalf("Failed to add recordsets: %v", err)
	}
}

/* TestZonesList verifies that listing of zones succeeds */
func TestZonesList(t *testing.T) {
	firstZone(t)
}

/* TestZonesID verifies that the id of the zone is returned with the prefix removed */
func TestZonesID(t *testing.T) {
	zone := firstZone(t)

	// Check /hostedzone/ prefix is removed
	zoneID := zone.ID()
	if zoneID != zone.Name() {
		t.Fatalf("Unexpected zone id: %q", zoneID)
	}
}

/* TestZoneAddSuccess verifies that addition of a valid managed DNS zone succeeds */
func TestZoneAddSuccess(t *testing.T) {
	testZoneName := "ubernetes.testing"
	z := zones(t)
	input, err := z.New(testZoneName)
	if err != nil {
		t.Errorf("Failed to allocate new zone object %s: %v", testZoneName, err)
	}
	zone, err := z.Add(input)
	if err != nil {
		t.Errorf("Failed to create new managed DNS zone %s: %v", testZoneName, err)
	}
	defer func(zone dnsprovider.Zone) {
		if zone != nil {
			if err := z.Remove(zone); err != nil {
				t.Errorf("Failed to delete zone %v: %v", zone, err)
			}
		}
	}(zone)
	t.Logf("Successfully added managed DNS zone: %v", zone)
}

/* TestResourceRecordSetsList verifies that listing of RRS's succeeds */
func TestResourceRecordSetsList(t *testing.T) {
	listRrsOrFail(t, rrs(t, firstZone(t)))
}

/* TestResourceRecordSetsAddSuccess verifies that addition of a valid RRS succeeds */
func TestResourceRecordSetsAddSuccess(t *testing.T) {
	ctx := context.Background()

	zone := firstZone(t)
	sets := rrs(t, zone)
	set := getExampleRrs(zone)
	addRrsetOrFail(ctx, t, sets, set)
	defer sets.StartChangeset().Remove(set).Apply(ctx)
	t.Logf("Successfully added resource record set: %v", set)
}

/* TestResourceRecordSetsAdditionVisible verifies that added RRS is visible after addition */
func TestResourceRecordSetsAdditionVisible(t *testing.T) {
	ctx := context.Background()

	zone := firstZone(t)
	sets := rrs(t, zone)
	rrset := getExampleRrs(zone)
	addRrsetOrFail(ctx, t, sets, rrset)
	defer sets.StartChangeset().Remove(rrset).Apply(ctx)
	t.Logf("Successfully added resource record set: %v", rrset)
	found := false
	for _, record := range listRrsOrFail(t, sets) {
		if record.Name() == rrset.Name() {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Failed to find added resource record set %s", rrset.Name())
	}
}

/* TestResourceRecordSetsAddDuplicateFailure verifies that addition of a duplicate RRS fails */
func TestResourceRecordSetsAddDuplicateFailure(t *testing.T) {
	ctx := context.Background()

	zone := firstZone(t)
	sets := rrs(t, zone)
	rrset := getExampleRrs(zone)
	addRrsetOrFail(ctx, t, sets, rrset)
	defer sets.StartChangeset().Remove(rrset).Apply(ctx)
	t.Logf("Successfully added resource record set: %v", rrset)
	// Try to add it again, and verify that the call fails.
	err := sets.StartChangeset().Add(rrset).Apply(ctx)
	if err == nil {
		defer sets.StartChangeset().Remove(rrset).Apply(ctx)
		t.Errorf("Should have failed to add duplicate resource record %v, but succeeded instead.", rrset)
	} else {
		t.Logf("Correctly failed to add duplicate resource record %v: %v", rrset, err)
	}
}

/* TestResourceRecordSetsRemove verifies that the removal of an existing RRS succeeds */
func TestResourceRecordSetsRemove(t *testing.T) {
	ctx := context.Background()

	zone := firstZone(t)
	sets := rrs(t, zone)
	rrset := getExampleRrs(zone)
	addRrsetOrFail(ctx, t, sets, rrset)
	err := sets.StartChangeset().Remove(rrset).Apply(ctx)
	if err != nil {
		// Try again to clean up.
		defer sets.StartChangeset().Remove(rrset).Apply(ctx)
		t.Errorf("Failed to remove resource record set %v after adding", rrset)
	} else {
		t.Logf("Successfully removed resource set %v after adding", rrset)
	}
}

/* TestResourceRecordSetsRemoveGone verifies that a removed RRS no longer exists */
func TestResourceRecordSetsRemoveGone(t *testing.T) {
	ctx := context.Background()

	zone := firstZone(t)
	sets := rrs(t, zone)
	rrset := getExampleRrs(zone)
	addRrsetOrFail(ctx, t, sets, rrset)
	err := sets.StartChangeset().Remove(rrset).Apply(ctx)
	if err != nil {
		// Try again to clean up.
		defer sets.StartChangeset().Remove(rrset).Apply(ctx)
		t.Errorf("Failed to remove resource record set %v after adding", rrset)
	} else {
		t.Logf("Successfully removed resource set %v after adding", rrset)
	}
	// Check that it's gone
	list := listRrsOrFail(t, sets)
	found := false
	for _, set := range list {
		if set.Name() == rrset.Name() {
			found = true
			break
		}
	}
	if found {
		t.Errorf("Deleted resource record set %v is still present", rrset)
	}
}

/* TestResourceRecordSetsReplace verifies that replacing an RRS works */
func TestResourceRecordSetsReplace(t *testing.T) {
	zone := firstZone(t)
	tests.CommonTestResourceRecordSetsReplace(t, zone)
}

/* TestResourceRecordSetsReplaceAll verifies that we can remove an RRS and create one with a different name*/
func TestResourceRecordSetsReplaceAll(t *testing.T) {
	zone := firstZone(t)
	tests.CommonTestResourceRecordSetsReplaceAll(t, zone)
}

/* TestResourceRecordSetsDifferentTypes verifies that we can add records of the same name but different types */
func TestResourceRecordSetsDifferentTypes(t *testing.T) {
	zone := firstZone(t)
	tests.CommonTestResourceRecordSetsDifferentTypes(t, zone)
}

// TestContract verifies the general interface contract
func TestContract(t *testing.T) {
	zone := firstZone(t)
	sets := rrs(t, zone)

	tests.TestContract(t, sets)
}
