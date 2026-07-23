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
