package storage

import (
	"strings"
	"testing"

	"backend/internal/media"
)

func TestListingImageObjectKeySafety(t *testing.T) {
	owner, err := media.NewID()
	if err != nil {
		t.Fatal(err)
	}
	asset, err := media.NewID()
	if err != nil {
		t.Fatal(err)
	}
	random := strings.Repeat("ab", 16)

	key, err := ListingImageObjectKey(owner.String(), asset.String(), random)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := "media/listing-images/" + owner.String() + "/" + asset.String() + "/"
	if !strings.HasPrefix(key, wantPrefix) {
		t.Fatalf("key = %q", key)
	}
	if strings.Contains(key, "photo.jpg") || strings.Contains(key, "..") {
		t.Fatalf("unsafe key %q", key)
	}
	if !media.IsServerObjectKey(key) {
		t.Fatalf("domain rejected key %q", key)
	}

	rejects := []struct{ owner, asset, random string }{
		{"../etc", asset.String(), random},
		{owner.String(), "evil.jpg", random},
		{owner.String(), asset.String(), "photo.jpg"},
		{owner.String() + "/../x", asset.String(), random},
		{"", asset.String(), random},
	}
	for _, c := range rejects {
		if _, err := ListingImageObjectKey(c.owner, c.asset, c.random); err == nil {
			t.Fatalf("accepted unsafe parts %+v", c)
		}
	}
}
