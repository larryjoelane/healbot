package azureid

import "testing"

func TestParseWebSite(t *testing.T) {
	id := "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Web/sites/example-api"
	ref, err := Parse(id)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Subscription != "sub" || ref.Group != "rg" || ref.Name != "example-api" {
		t.Fatalf("%+v", ref)
	}
	if ref.ResourceType() != "Microsoft.Web/sites" {
		t.Fatal(ref.ResourceType())
	}
}

func TestParseContainerRevision(t *testing.T) {
	id := "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.App/containerApps/shop/revisions/shop--abc"
	ref, err := Parse(id)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Name != "shop" || ref.SubType != "revisions" || ref.SubName != "shop--abc" {
		t.Fatalf("%+v", ref)
	}
}

func TestParseRejectsShort(t *testing.T) {
	if _, err := Parse("/subscriptions/sub"); err == nil {
		t.Fatal("expected error")
	}
}
