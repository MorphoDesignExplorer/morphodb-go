package morphoroutes

import (
	"encoding/json"
	"fmt"
)

/*
	This is a collection of methods to support the Scan interface for types
	in db_types.go.

	This is supposed to be used with the rows.Scan() method returned by a query
	from the database/sql package.
*/

func (c *Captions) Scan(src any) error {
	if srcString, ok := src.(string); ok {
		err := json.Unmarshal([]byte(srcString), c)
		if err != nil {
			return fmt.Errorf("Error while parsing value into []Caption: %s", err.Error())
		}
		return nil
	} else {
		return fmt.Errorf("The field was not a string")
	}
}

func (p *ProjectMetadataFields) Scan(src any) error {
	if srcString, ok := src.(string); ok {
		err := json.Unmarshal([]byte(srcString), p)
		if err != nil {
			return fmt.Errorf("Error while parsing column into []ProjectMetadataField: %s", err.Error())
		}
		return nil
	} else {
		return fmt.Errorf("Field was not bytes.")
	}
}

func (p *ProjectAssetFields) Scan(src any) error {
	if srcString, ok := src.(string); ok {
		err := json.Unmarshal([]byte(srcString), p)
		if err != nil {
			return fmt.Errorf("Error while parsing value into []ProjectAssetField: %s", err.Error())
		}
		return nil
	} else {
		return fmt.Errorf("The field was not a string.")
	}
}

func (d *DoubleMap) Scan(src any) error {
	if srcString, ok := src.(string); ok {
		err := json.Unmarshal([]byte(srcString), d)
		if err != nil {
			return fmt.Errorf("Error while parsing value into map[string]float64: %s", err.Error())
		}
		return nil
	} else {
		return fmt.Errorf("The field was not a string.")
	}
}
