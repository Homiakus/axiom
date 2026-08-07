package runtime

func resolveRuntimeRef(execution *Execution, field string) any {
	if execution == nil {
		return nil
	}
	switch field {
	case "id":
		return execution.ID
	case "domain":
		return execution.Domain
	case "status":
		return execution.Status
	case "version":
		return execution.Version
	case "createdAt":
		return execution.CreatedAt
	case "updatedAt":
		return execution.UpdatedAt
	case "moduleHash":
		return execution.ModuleHash
	case "compilerVersion":
		return execution.CompilerVersion
	case "planVersion":
		return execution.PlanVersion
	default:
		return nil
	}
}
