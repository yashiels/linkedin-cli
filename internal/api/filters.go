package api

import (
	"encoding/json"
	"fmt"
)

const (
	queryFilters   = "JobSearchFiltersByJobSeachQuery"
	queryIDFilters = "voyagerJobsDashSearchFilterClustersResource.47b05823e4f9f731229151a0c3b4aa87"
)

// FilterCluster is a group of related search filters returned by the API.
type FilterCluster struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Filters []SearchFilter `json:"filters"`
}

// SearchFilter is a single selectable filter option.
type SearchFilter struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// FetchFilters retrieves available search filter clusters for a query.
func (c *Client) FetchFilters(keywords, geoURN string) ([]FilterCluster, error) {
	query := map[string]interface{}{
		"keywords": keywords,
		"origin":   "JOB_SEARCH_PAGE_SEARCH_BUTTON",
		"selectedFilters": map[string]interface{}{
			"resultType": []string{"JOBS"},
		},
		"spellCorrectionEnabled": true,
	}
	if geoURN != "" {
		query["locationUnion"] = map[string]interface{}{
			"geoUrn": geoURN,
		}
	}

	vars := map[string]interface{}{
		"query": query,
		"count": 200,
		"start": 0,
	}

	raw, err := c.QueryGraphQL(queryFilters, queryIDFilters, vars)
	if err != nil {
		return nil, fmt.Errorf("filters: query failed: %w", err)
	}

	return parseFilters(raw)
}

// parseFilters extracts FilterCluster data from a raw Voyager response.
func parseFilters(raw json.RawMessage) ([]FilterCluster, error) {
	// Try shape 1: {data: {data: {jobSearchFiltersByJobSeachQuery: {elements: [...]}}}}
	var shape1 struct {
		Data struct {
			Data struct {
				Filters *struct {
					Elements []json.RawMessage `json:"elements"`
				} `json:"jobSearchFiltersByJobSeachQuery"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &shape1); err == nil {
		if shape1.Data.Data.Filters != nil {
			return decodeFilterElements(shape1.Data.Data.Filters.Elements), nil
		}
	}

	// Try shape 2: {data: {elements: [...]}}
	var shape2 struct {
		Data struct {
			Elements []json.RawMessage `json:"elements"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &shape2); err == nil && len(shape2.Data.Elements) > 0 {
		return decodeFilterElements(shape2.Data.Elements), nil
	}

	return nil, nil
}

// decodeFilterElements converts raw filter cluster elements.
func decodeFilterElements(elements []json.RawMessage) []FilterCluster {
	clusters := make([]FilterCluster, 0, len(elements))
	for _, elem := range elements {
		var cluster struct {
			EntityURN string `json:"entityUrn"`
			Name      struct {
				Text string `json:"text"`
			} `json:"name"`
			FilterValues []struct {
				EntityURN    string `json:"entityUrn"`
				DisplayValue struct {
					Text string `json:"text"`
				} `json:"displayValue"`
				FilterCounts int `json:"filterCounts"`
			} `json:"filterValues"`
		}
		if err := json.Unmarshal(elem, &cluster); err != nil {
			continue
		}

		fc := FilterCluster{
			ID:      cluster.EntityURN,
			Name:    cluster.Name.Text,
			Filters: make([]SearchFilter, 0, len(cluster.FilterValues)),
		}
		for _, fv := range cluster.FilterValues {
			fc.Filters = append(fc.Filters, SearchFilter{
				ID:    fv.EntityURN,
				Name:  fv.DisplayValue.Text,
				Count: fv.FilterCounts,
			})
		}
		clusters = append(clusters, fc)
	}
	return clusters
}
