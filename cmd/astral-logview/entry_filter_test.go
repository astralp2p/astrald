package main

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

func TestEntryFilter(t *testing.T) {
	tag := log.Tag("nodes")
	other := log.Tag("other")

	tests := []struct {
		name   string
		filter EntryFilter
		entry  *log.Entry
		want   bool
	}{
		{
			name:   "tag match ignores level",
			filter: EntryFilter{Tag: "nodes"},
			entry:  &log.Entry{Level: 9, Objects: []astral.Object{&tag}},
			want:   true,
		},
		{
			name:   "tag missing",
			filter: EntryFilter{Tag: "nodes"},
			entry:  &log.Entry{Level: 0, Objects: []astral.Object{&other}},
			want:   false,
		},
		{
			name:   "level above filter",
			filter: EntryFilter{Level: 1},
			entry:  &log.Entry{Level: 2},
			want:   false,
		},
		{
			name:   "level at filter",
			filter: EntryFilter{Level: 1},
			entry:  &log.Entry{Level: 1},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.filter.Filter(tt.entry); got != tt.want {
				t.Fatalf("%+v.Filter(level %d) = %v, want %v", tt.filter, tt.entry.Level, got, tt.want)
			}
		})
	}
}
