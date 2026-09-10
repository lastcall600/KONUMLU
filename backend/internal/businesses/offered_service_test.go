package businesses

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestNewDraftOfferedServicePreservesUnicode(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	got, err := NewDraftOfferedService(mustID(t), ServiceContent{
		Title:       "  Tur Кафе  ",
		Description: "  Açıklama — описание  ",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ServiceStatusDraft || got.Title != "Tur Кафе" || got.Description != "Açıklama — описание" {
		t.Fatalf("svc = %+v", got)
	}
}

func TestOfferedServiceRejectsEmptyAndOverlong(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	biz := mustID(t)
	if _, err := NewDraftOfferedService(biz, ServiceContent{Title: "  "}, now); !errors.Is(err, errInvalidContent) {
		t.Fatalf("blank err = %v", err)
	}
	if _, err := NewDraftOfferedService(biz, ServiceContent{Title: strings.Repeat("a", MaxServiceTitleRunes+1)}, now); !errors.Is(err, errInvalidContent) {
		t.Fatalf("title len err = %v", err)
	}
	ok, err := NewDraftOfferedService(biz, ServiceContent{Title: strings.Repeat("م", MaxServiceTitleRunes)}, now)
	if err != nil {
		t.Fatal(err)
	}
	if utf8.RuneCountInString(ok.Title) != MaxServiceTitleRunes {
		t.Fatalf("title runes = %d", utf8.RuneCountInString(ok.Title))
	}
}

func TestOfferedServiceLifecycle(t *testing.T) {
	s := mustDraftService(t)
	at := s.CreatedAt.Add(time.Minute)
	if _, err := s.Pause(at); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("pause draft err = %v", err)
	}
	active, err := s.Activate(at)
	if err != nil {
		t.Fatal(err)
	}
	if active.Status != ServiceStatusActive {
		t.Fatalf("status = %s", active.Status)
	}
	if _, err := active.Activate(at.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("double activate err = %v", err)
	}
	paused, err := active.Pause(at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if paused.Status != ServiceStatusPaused {
		t.Fatalf("status = %s", paused.Status)
	}
	reactivated, err := paused.Activate(paused.UpdatedAt.Add(time.Second))
	if err != nil || reactivated.Status != ServiceStatusActive {
		t.Fatalf("reactivate = %+v err = %v", reactivated, err)
	}
	closed, err := reactivated.Close(reactivated.UpdatedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != ServiceStatusClosed {
		t.Fatalf("status = %s", closed.Status)
	}
	if _, err := closed.Activate(closed.UpdatedAt.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("reopen err = %v", err)
	}
	if _, err := closed.Pause(closed.UpdatedAt.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("pause closed err = %v", err)
	}
	if _, err := closed.Close(closed.UpdatedAt.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("double close err = %v", err)
	}
	if _, err := closed.UpdateContent(ServiceContent{Title: "X"}, closed.UpdatedAt.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("patch closed err = %v", err)
	}
}

func TestCloseDraftAndPaused(t *testing.T) {
	draft := mustDraftService(t)
	closed, err := draft.Close(draft.CreatedAt.Add(time.Second))
	if err != nil || closed.Status != ServiceStatusClosed {
		t.Fatalf("close draft = %+v err = %v", closed, err)
	}
	paused := mustDraftService(t)
	active, err := paused.Activate(paused.CreatedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	paused, err = active.Pause(active.UpdatedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	closed, err = paused.Close(paused.UpdatedAt.Add(time.Second))
	if err != nil || closed.Status != ServiceStatusClosed {
		t.Fatalf("close paused = %+v err = %v", closed, err)
	}
}

func TestServicePriceValidation(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	biz := mustID(t)
	amount := "120.50"
	currency := "TRY"
	ok, err := NewDraftOfferedService(biz, ServiceContent{
		Title: "Tur",
		Price: ServicePrice{Model: PriceModelFixed, Amount: &amount, Currency: &currency},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if ok.Price.Model != PriceModelFixed || *ok.Price.Amount != "120.50" || *ok.Price.Currency != "TRY" {
		t.Fatalf("price = %+v", ok.Price)
	}
	starting := "80"
	if _, err := NewDraftOfferedService(biz, ServiceContent{
		Title: "Tur",
		Price: ServicePrice{Model: PriceModelStartingFrom, Amount: &starting, Currency: &currency},
	}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDraftOfferedService(biz, ServiceContent{
		Title: "Tur",
		Price: ServicePrice{Model: PriceModelQuoteRequired},
	}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDraftOfferedService(biz, ServiceContent{
		Title: "Tur",
		Price: ServicePrice{Model: PriceModelFixed, Amount: &amount},
	}, now); !errors.Is(err, errInvalidPrice) {
		t.Fatalf("fixed without currency err = %v", err)
	}
	if _, err := NewDraftOfferedService(biz, ServiceContent{
		Title: "Tur",
		Price: ServicePrice{Model: PriceModelQuoteRequired, Amount: &amount, Currency: &currency},
	}, now); !errors.Is(err, errInvalidPrice) {
		t.Fatalf("quote with amount err = %v", err)
	}
	if _, err := NewDraftOfferedService(biz, ServiceContent{
		Title: "Tur",
		Price: ServicePrice{Model: PriceModelNone, Amount: &amount, Currency: &currency},
	}, now); !errors.Is(err, errInvalidPrice) {
		t.Fatalf("none with amount err = %v", err)
	}
	badCur := "try"
	if _, err := NewDraftOfferedService(biz, ServiceContent{
		Title: "Tur",
		Price: ServicePrice{Model: PriceModelFixed, Amount: &amount, Currency: &badCur},
	}, now); !errors.Is(err, errInvalidPrice) {
		t.Fatalf("lowercase currency err = %v", err)
	}
	badAmt := "12.123456789"
	if _, err := NewDraftOfferedService(biz, ServiceContent{
		Title: "Tur",
		Price: ServicePrice{Model: PriceModelFixed, Amount: &badAmt, Currency: &currency},
	}, now); !errors.Is(err, errInvalidPrice) {
		t.Fatalf("overprecise amount err = %v", err)
	}
	if _, err := NewDraftOfferedService(biz, ServiceContent{
		Title: "Tur",
		Price: ServicePrice{Model: "hourly"},
	}, now); !errors.Is(err, errInvalidPrice) {
		t.Fatalf("unknown model err = %v", err)
	}
}

func TestOfferedServicePubliclyReadable(t *testing.T) {
	s := mustDraftService(t)
	if s.PubliclyReadable() {
		t.Fatal("draft public")
	}
	active, err := s.Activate(s.CreatedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !active.PubliclyReadable() {
		t.Fatal("active should be public")
	}
	paused, err := active.Pause(active.UpdatedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if paused.PubliclyReadable() {
		t.Fatal("paused public")
	}
}

func mustDraftService(t *testing.T) OfferedService {
	t.Helper()
	s, err := NewDraftOfferedService(mustID(t), ServiceContent{Title: "Tur", Description: "Sahil"}, time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
