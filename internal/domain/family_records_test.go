package domain

import (
	"github.com/stretchr/testify/require"
	"math"
	"testing"
	"time"
)

func TestFamilyRecordValidation(t *testing.T) {
	valid := FamilyRecordInput{MemberID: "abu", Category: "health", OccurredAt: time.Now(), Title: "测量", Measurements: []FamilyMeasurement{{Metric: "temperature", Value: 36.7, Unit: "°C"}}}
	require.NoError(t, valid.Validate())
	cases := []struct {
		name string
		edit func(*FamilyRecordInput)
	}{
		{"path as member", func(r *FamilyRecordInput) { r.MemberID = "../abu" }},
		{"missing time", func(r *FamilyRecordInput) { r.OccurredAt = time.Time{} }},
		{"blank title", func(r *FamilyRecordInput) { r.Title = "  " }},
		{"unknown category", func(r *FamilyRecordInput) { r.Category = "diagnosis" }},
		{"nonfinite value", func(r *FamilyRecordInput) {
			r.Measurements = []FamilyMeasurement{{Metric: "weight", Value: math.NaN(), Unit: "kg"}}
		}},
		{"missing unit", func(r *FamilyRecordInput) { r.Measurements = []FamilyMeasurement{{Metric: "weight", Value: 7}} }},
		{"duplicate metric", func(r *FamilyRecordInput) { r.Measurements = append(r.Measurements, r.Measurements[0]) }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) { r := valid; tt.edit(&r); require.Error(t, r.Validate()) })
	}
}
