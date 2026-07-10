package app

import "testing"

func TestToggleWarningDetails(t *testing.T) {
	m := &SwapUI{showWarningDetails: true}

	m.toggleWarningDetails()
	if m.showWarningDetails {
		t.Fatalf("showWarningDetails should be false after first toggle")
	}

	m.toggleWarningDetails()
	if !m.showWarningDetails {
		t.Fatalf("showWarningDetails should be true after second toggle")
	}
}

func TestWarningDetailsLabel(t *testing.T) {
	m := &SwapUI{showWarningDetails: true}
	if got := m.warningDetailsLabel(); got != "Warnings: ON" {
		t.Fatalf("warningDetailsLabel()=%q want=%q", got, "Warnings: ON")
	}

	m.showWarningDetails = false
	if got := m.warningDetailsLabel(); got != "Warnings: OFF" {
		t.Fatalf("warningDetailsLabel()=%q want=%q", got, "Warnings: OFF")
	}
}
