package main

import (
	"testing"

	cfg "github.com/conductorone/baton-azure-infrastructure/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/field"
	"github.com/conductorone/baton-sdk/pkg/test"
)

func TestConfigs(t *testing.T) {
	configurationSchema := field.NewConfiguration(
		cfg.ConfigurationFields,
		field.WithConstraints(cfg.FieldRelationships...),
	)

	testCases := []test.TestCase{
		// Add test cases here.
	}

	test.ExerciseTestCases(t, configurationSchema, ValidateConfig, testCases)
}
