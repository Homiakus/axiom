package generated

import (
	"context"
	"fmt"
)

type HydroPilotActivityImpl struct{}

func (HydroPilotActivityImpl) CalculateDosePlan(ctx context.Context, input CalculateDosePlanInput) (CalculateDosePlanOutput, error) {
	return CalculateDosePlanOutput{}, fmt.Errorf("CalculateDosePlan is not implemented")
}

func (HydroPilotActivityImpl) NotifyOperator(ctx context.Context, input NotifyOperatorInput) (NotifyOperatorOutput, error) {
	return NotifyOperatorOutput{}, fmt.Errorf("NotifyOperator is not implemented")
}

func (HydroPilotActivityImpl) ReadZoneSnapshot(ctx context.Context, input ReadZoneSnapshotInput) (ReadZoneSnapshotOutput, error) {
	return ReadZoneSnapshotOutput{}, fmt.Errorf("ReadZoneSnapshot is not implemented")
}

func (HydroPilotActivityImpl) SendUartRequest(ctx context.Context, input SendUartRequestInput) (SendUartRequestOutput, error) {
	return SendUartRequestOutput{}, fmt.Errorf("SendUartRequest is not implemented")
}

func (HydroPilotActivityImpl) StoreCalibration(ctx context.Context, input StoreCalibrationInput) (StoreCalibrationOutput, error) {
	return StoreCalibrationOutput{}, fmt.Errorf("StoreCalibration is not implemented")
}
