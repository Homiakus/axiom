package hydropilot

import (
	"context"
	"fmt"
)

type HydroPilotActivityImpl struct{}

func (HydroPilotActivityImpl) BuildCalibrationUartRequest(ctx context.Context, input BuildCalibrationUartRequestInput) (BuildCalibrationUartRequestOutput, error) {
	return BuildCalibrationUartRequestOutput{}, fmt.Errorf("BuildCalibrationUartRequest is not implemented")
}

func (HydroPilotActivityImpl) BuildLightingUartRequest(ctx context.Context, input BuildLightingUartRequestInput) (BuildLightingUartRequestOutput, error) {
	return BuildLightingUartRequestOutput{}, fmt.Errorf("BuildLightingUartRequest is not implemented")
}

func (HydroPilotActivityImpl) BuildMeasurementUartRequest(ctx context.Context, input BuildMeasurementUartRequestInput) (BuildMeasurementUartRequestOutput, error) {
	return BuildMeasurementUartRequestOutput{}, fmt.Errorf("BuildMeasurementUartRequest is not implemented")
}

// Расчет плана решает, нужна ли pH-коррекция, питание или разбавление TDS.
func (HydroPilotActivityImpl) CalculateDosePlan(ctx context.Context, input CalculateDosePlanInput) (CalculateDosePlanOutput, error) {
	return CalculateDosePlanOutput{}, fmt.Errorf("CalculateDosePlan is not implemented")
}

func (HydroPilotActivityImpl) NotifyOperator(ctx context.Context, input NotifyOperatorInput) (NotifyOperatorOutput, error) {
	return NotifyOperatorOutput{}, fmt.Errorf("NotifyOperator is not implemented")
}

func (HydroPilotActivityImpl) ParseZoneMeasurement(ctx context.Context, input ParseZoneMeasurementInput) (ParseZoneMeasurementOutput, error) {
	return ParseZoneMeasurementOutput{}, fmt.Errorf("ParseZoneMeasurement is not implemented")
}

// Все actuator-команды идут через один UART-шлюз.
func (HydroPilotActivityImpl) SendUartRequest(ctx context.Context, input SendUartRequestInput) (SendUartRequestOutput, error) {
	return SendUartRequestOutput{}, fmt.Errorf("SendUartRequest is not implemented")
}

func (HydroPilotActivityImpl) StoreCalibration(ctx context.Context, input StoreCalibrationInput) (StoreCalibrationOutput, error) {
	return StoreCalibrationOutput{}, fmt.Errorf("StoreCalibration is not implemented")
}

func (HydroPilotActivityImpl) StoreZoneMeasurement(ctx context.Context, input StoreZoneMeasurementInput) (StoreZoneMeasurementOutput, error) {
	return StoreZoneMeasurementOutput{}, fmt.Errorf("StoreZoneMeasurement is not implemented")
}
