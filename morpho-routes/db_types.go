package morphoroutes

import (
	"database/sql"
	"fmt"
	"runtime"
)

/*
	This module describes the data types used by the database and the application.

	Check validator.go for more information on the validate struct tag.
*/

type Description struct {
	Slug string `json:"slug" validate:"zero"`
	Text string `json:"text" validate:"zero"`
}

type Caption struct {
	Tagname     string `json:"tag_name" validate:"zero"`
	DisplayName string `json:"display_name" validate:"zero"`
}

type Captions []Caption

type Metadata struct {
	Captions    Captions    `json:"captions"`
	Description Description `json:"description" validate:"zero"`
	HumanName   string      `json:"human_name" validate:"zero"`
}

type ProjectMetadataField struct {
	FieldName      string    `json:"field_name" validate:"zero"`
	FieldStep      float64   `json:"field_step,omitempty"` // omittable
	FieldType      string    `json:"field_type" validate:"zero"`
	FieldUnit      string    `json:"field_unit" validate:"zero"`
	FieldRange     []float64 `json:"field_range,omitempty"`     // omittable
	FieldPrecision float64   `json:"field_precision,omitempty"` // omittable
}

type ProjectMetadataFields []ProjectMetadataField

type ProjectAssetField struct {
	Description string `json:"description" validate:"zero"`
	Extension   string `json:"extension" validate:"zero"`
	MimeType    string `json:"mime_type" validate:"zero"`
	Tag         string `json:"tag" validate:"zero"`
}

type ProjectAssetFields []ProjectAssetField

type Project struct {
	CreationDate     string                `json:"creation_date" validate:"zero"`
	ProjectName      string                `json:"project_name" validate:"zero"`
	VariableMetadata ProjectMetadataFields `json:"variable_metadata" validate:"zero"`
	OutputMetadata   ProjectMetadataFields `json:"output_metadata" validate:"zero"`
	Assets           ProjectAssetFields    `json:"assets" validate:"zero"`
	Deleted          bool                  `json:"deleted"`
	ProjectMetadata  Metadata              `json:"metadata"`
}

type DoubleMap map[string]float64

type Solution struct {
	Id              string    `json:"id" validate:"zero"`
	ScopedId        string    `json:"scoped_id" validate:"zero"`
	Parameter       DoubleMap `json:"parameters" validate:"zero"`
	OutputParameter DoubleMap `json:"output_parameters" validate:"zero"`
	Assets          []Asset   `json:"files,omitempty"`
}

type SolutionSet []Solution

// Represents an Asset of a solution.
type Asset struct {
	Tag  string `json:"tag" validate:"zero"`
	File string `json:"file" validate:"zero"`
}

// Represents a generic document on the database.
type Document struct {
	Id   string `json:"id" validate:"zero"`
	Slug string `json:"slug" validate:"zero"`
	Text string `json:"text" validate:"zero"`
}

// Common Functionality

// provides different drivers depending on the build platform (windows or unix).
func GetDriver() string {
	if runtime.GOOS == "windows" {
		return "sqlite"
	} else {
		return "sqlite3"
	}
}

// Starts and returns a SQLite DB connection.
func StartConn(config Config) (*sql.DB, error) {
	connString := fmt.Sprintf("file:%s?_journal:WAL", config.DB_STRING)

	db, err := sql.Open(GetDriver(), connString)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec("pragma journal_mode=wal;")
	if err != nil {
		return nil, err
	}

	return db, nil
}
