package store

import "encoding/json"

func decodeReviewRun(data []byte) (ReviewRunV1, error) {
	var run ReviewRunV1
	if err := json.Unmarshal(data, &run); err != nil {
		return ReviewRunV1{}, err
	}
	if _, _, err := FromReviewRunSchema(run); err != nil {
		return ReviewRunV1{}, err
	}
	return run, nil
}

func decodeReviewEvent(data []byte) (ReviewEventV1, error) {
	var event ReviewEventV1
	if err := json.Unmarshal(data, &event); err != nil {
		return ReviewEventV1{}, err
	}
	if err := event.Validate(); err != nil {
		return ReviewEventV1{}, err
	}
	return event, nil
}

func decodePRSnapshot(data []byte) (PRSnapshotV1, error) {
	var snapshot PRSnapshotV1
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return PRSnapshotV1{}, err
	}
	if err := snapshot.Validate(); err != nil {
		return PRSnapshotV1{}, err
	}
	return snapshot, nil
}

func decodeAdjudicationRecord(data []byte) (AdjudicationRecordV1, error) {
	var record AdjudicationRecordV1
	if err := json.Unmarshal(data, &record); err != nil {
		return AdjudicationRecordV1{}, err
	}
	if err := record.Validate(); err != nil {
		return AdjudicationRecordV1{}, err
	}
	return record, nil
}

func decodeLoopDecision(data []byte) (LoopDecisionV1, error) {
	var decision LoopDecisionV1
	if err := json.Unmarshal(data, &decision); err != nil {
		return LoopDecisionV1{}, err
	}
	if err := decision.Validate(); err != nil {
		return LoopDecisionV1{}, err
	}
	return decision, nil
}

func decodeReviewEconomics(data []byte) (ReviewEconomicsV1, error) {
	var economics ReviewEconomicsV1
	if err := json.Unmarshal(data, &economics); err != nil {
		return ReviewEconomicsV1{}, err
	}
	if err := economics.Validate(); err != nil {
		return ReviewEconomicsV1{}, err
	}
	return economics, nil
}
