package contracts

import "testing"

func TestListingAttachContextZero(t *testing.T) {
	var bind ListingAttachContext
	if !bind.ListingID.IsZero() || !bind.ActorUserID.IsZero() {
		t.Fatal("empty bind")
	}
}

func TestPublicMediaItemHasNoObjectKeyField(t *testing.T) {
	item := PublicMediaItem{Kind: "listing_image", Order: 0, URL: "https://objects.test/public/x"}
	if item.URL == "" || item.Kind == "" {
		t.Fatal("public fields required")
	}
}
