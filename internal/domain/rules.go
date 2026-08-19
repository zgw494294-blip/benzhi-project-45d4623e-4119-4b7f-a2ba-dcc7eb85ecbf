package domain

import "time"

func (p InspectionPlan) NextSite() *InspectionSite {
	for i := range p.Sites {
		if p.Sites[i].Status != SiteStatusCompleted {
			copySite := cloneSite(p.Sites[i])
			return &copySite
		}
	}
	return nil
}

func (p InspectionPlan) ExceptionCount() int {
	count := 0
	for _, site := range p.Sites {
		for _, observation := range site.Observations {
			if observation.Severity != SeverityNormal {
				count++
			}
		}
	}
	return count
}

func (p InspectionPlan) PendingReviewCount() int {
	count := 0
	for _, site := range p.Sites {
		for _, observation := range site.Observations {
			if observation.ReviewStatus == ReviewPending {
				count++
			}
		}
	}
	return count
}

func (p InspectionPlan) CompletedSiteCount() int {
	count := 0
	for _, site := range p.Sites {
		if site.Status == SiteStatusCompleted {
			count++
		}
	}
	return count
}

func (o Observation) IsException() bool {
	return o.Severity != SeverityNormal
}

func (o Observation) IsReviewed() bool {
	return o.ReviewStatus == ReviewApproved || o.ReviewStatus == ReviewRejected
}

func cloneSite(site InspectionSite) InspectionSite {
	site.Observations = append([]Observation(nil), site.Observations...)
	return site
}

func NewObservation(id, siteID, kind, value, unit, note, observer string, severity Severity, observedAt time.Time, idempotencyKey string) (Observation, error) {
	observation := Observation{
		ID: id, SiteID: siteID, Kind: kind, Value: value, Unit: unit, Note: note,
		Observer: observer, Severity: severity, ObservedAt: observedAt.UTC(), IdempotencyKey: idempotencyKey,
		ReviewHistory: []ReviewEvent{},
	}
	if severity == SeverityNormal {
		observation.ReviewStatus = ReviewNotRequired
	} else {
		observation.ReviewStatus = ReviewPending
	}
	if err := validateObservation(observation); err != nil {
		return Observation{}, err
	}
	return observation, nil
}
