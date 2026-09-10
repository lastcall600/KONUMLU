"use client";

import { useEffect, useRef } from "react";
import {
  LngLatBounds,
  Map as MapLibreMap,
  Marker,
  NavigationControl,
  Popup,
} from "maplibre-gl";
import "maplibre-gl/dist/maplibre-gl.css";

import {
  hasValidCoordinates,
  type SearchListing,
  type SearchViewport,
} from "@/lib/search";

const MAP_STYLE = "https://tiles.openfreemap.org/styles/liberty";

const TURKEY_CENTER: [number, number] = [35.2, 39.0];
const TURKEY_ZOOM = 5;
const FIT_MAX_ZOOM = 11;
const FIT_PADDING = 48;

export type SearchMapProps = {
  listings: SearchListing[];
  selectedListingId: string | null;
  showPopup: boolean;
  cameraKey: string;
  cameraViewport?: SearchViewport;
  onSelectListing: (listingId: string) => void;
  onUserBoundsChange: (viewport: SearchViewport) => void;
  onUnavailable: () => void;
};

function formatPrice(listing: SearchListing): string | null {
  if (!listing.priceAmount) {
    return null;
  }
  if (listing.priceCurrency) {
    return `${listing.priceAmount} ${listing.priceCurrency}`;
  }
  return listing.priceAmount;
}

function listingTitle(listing: SearchListing): string {
  return listing.title === "" ? "Başlıksız ilan" : listing.title;
}

function popupNode(listing: SearchListing): HTMLElement {
  const root = document.createElement("div");
  root.className = "search-map-popup";

  const link = document.createElement("a");
  link.href = `/ilan/${encodeURIComponent(listing.listingId)}`;
  link.textContent = listingTitle(listing);
  root.appendChild(link);

  const price = formatPrice(listing);
  if (price) {
    const priceEl = document.createElement("p");
    priceEl.textContent = price;
    root.appendChild(priceEl);
  }

  return root;
}

export function SearchMap({
  listings,
  selectedListingId,
  showPopup,
  cameraKey,
  cameraViewport,
  onSelectListing,
  onUserBoundsChange,
  onUnavailable,
}: SearchMapProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const mapRef = useRef<MapLibreMap | null>(null);
  const markersRef = useRef<Map<string, Marker>>(new Map());
  const popupRef = useRef<Popup | null>(null);
  const ignoreMoveRef = useRef(0);
  const listingsRef = useRef(listings);
  const cameraViewportRef = useRef(cameraViewport);
  const onSelectRef = useRef(onSelectListing);
  const onBoundsRef = useRef(onUserBoundsChange);
  const onUnavailableRef = useRef(onUnavailable);
  listingsRef.current = listings;
  cameraViewportRef.current = cameraViewport;
  onSelectRef.current = onSelectListing;
  onBoundsRef.current = onUserBoundsChange;
  onUnavailableRef.current = onUnavailable;

  useEffect(() => {
    const container = containerRef.current;
    if (!container || mapRef.current) {
      return;
    }

    let map: MapLibreMap;
    try {
      map = new MapLibreMap({
        container,
        style: MAP_STYLE,
        center: TURKEY_CENTER,
        zoom: TURKEY_ZOOM,
      });
    } catch {
      onUnavailableRef.current();
      return;
    }

    let styleLoaded = false;
    map.addControl(new NavigationControl({ showCompass: false }), "top-right");
    map.on("error", () => {
      if (!styleLoaded) {
        onUnavailableRef.current();
      }
    });
    map.on("load", () => {
      styleLoaded = true;
      map.resize();
    });
    map.on("moveend", () => {
      if (ignoreMoveRef.current > 0) {
        ignoreMoveRef.current -= 1;
        return;
      }
      const bounds = map.getBounds();
      onBoundsRef.current({
        north: bounds.getNorth(),
        south: bounds.getSouth(),
        east: bounds.getEast(),
        west: bounds.getWest(),
      });
    });

    mapRef.current = map;
    return () => {
      popupRef.current?.remove();
      popupRef.current = null;
      for (const marker of markersRef.current.values()) {
        marker.remove();
      }
      markersRef.current.clear();
      map.remove();
      mapRef.current = null;
    };
  }, []);

  useEffect(() => {
    const map = mapRef.current;
    if (!map) {
      return;
    }

    const nextIds = new Set<string>();
    for (const listing of listings) {
      if (!hasValidCoordinates(listing) || listing.latitude === undefined || listing.longitude === undefined) {
        continue;
      }
      nextIds.add(listing.listingId);
      let marker = markersRef.current.get(listing.listingId);
      if (!marker) {
        const el = document.createElement("button");
        el.type = "button";
        el.className = "search-map-marker";
        el.setAttribute("aria-label", listingTitle(listing));
        const created = new Marker({ element: el, anchor: "bottom" });
        created.setLngLat([listing.longitude, listing.latitude]);
        created.addTo(map);
        el.addEventListener("click", (event) => {
          event.stopPropagation();
          onSelectRef.current(listing.listingId);
        });
        markersRef.current.set(listing.listingId, created);
      } else {
        marker.setLngLat([listing.longitude, listing.latitude]);
      }
    }

    for (const [id, marker] of markersRef.current) {
      if (!nextIds.has(id)) {
        marker.remove();
        markersRef.current.delete(id);
      }
    }
  }, [listings]);

  useEffect(() => {
    for (const [id, marker] of markersRef.current) {
      const el = marker.getElement();
      const selected = id === selectedListingId;
      el.classList.toggle("search-map-marker-selected", selected);
    }

    const map = mapRef.current;
    const selected = selectedListingId
      ? listings.find((listing) => listing.listingId === selectedListingId)
      : undefined;
    if (
      !showPopup ||
      !map ||
      !selected ||
      !hasValidCoordinates(selected) ||
      selected.latitude === undefined ||
      selected.longitude === undefined
    ) {
      popupRef.current?.remove();
      popupRef.current = null;
      return;
    }

    popupRef.current?.remove();
    popupRef.current = new Popup({ offset: 18, closeButton: true, closeOnClick: true })
      .setLngLat([selected.longitude, selected.latitude])
      .setDOMContent(popupNode(selected))
      .addTo(map);
  }, [listings, selectedListingId, showPopup]);

  useEffect(() => {
    const map = mapRef.current;
    if (!map) {
      return;
    }

    const applyCamera = () => {
      ignoreMoveRef.current += 1;
      const viewport = cameraViewportRef.current;
      if (viewport) {
        map.fitBounds(
          [
            [viewport.west, viewport.south],
            [viewport.east, viewport.north],
          ],
          { padding: FIT_PADDING, duration: 0, maxZoom: FIT_MAX_ZOOM },
        );
        return;
      }

      const points = listingsRef.current.filter(hasValidCoordinates);
      if (points.length === 0) {
        map.jumpTo({ center: TURKEY_CENTER, zoom: TURKEY_ZOOM });
        return;
      }

      const bounds = new LngLatBounds();
      for (const listing of points) {
        if (listing.longitude === undefined || listing.latitude === undefined) {
          continue;
        }
        bounds.extend([listing.longitude, listing.latitude]);
      }
      map.fitBounds(bounds, { padding: FIT_PADDING, duration: 0, maxZoom: FIT_MAX_ZOOM });
    };

    if (map.loaded()) {
      applyCamera();
      return;
    }
    map.once("load", applyCamera);
    return () => {
      map.off("load", applyCamera);
    };
  }, [cameraKey]);

  return <div ref={containerRef} className="search-map-canvas" role="presentation" />;
}
