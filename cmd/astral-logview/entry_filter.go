package main

import "github.com/astralp2p/astral-go/astral/log"

type EntryFilter struct {
	Tag   string
	Level uint
}

// Filter returns true if the entry matches the filter
func (e EntryFilter) Filter(entry *log.Entry) bool {
	if e.Tag != "" {
		for _, o := range entry.Objects {
			switch o := o.(type) {
			case *log.Tag:
				if o.String() == e.Tag {
					return true
				}
			}
		}
		return false
	}

	if entry.Level > uint8(e.Level) {
		return false
	}

	return true
}
