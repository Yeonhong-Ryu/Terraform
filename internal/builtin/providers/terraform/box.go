package terraform

import (
	"fmt"

	"github.com/hashicorp/go-uuid"
	"github.com/hashicorp/terraform/internal/configs/configschema"
	"github.com/hashicorp/terraform/internal/providers"
	"github.com/hashicorp/terraform/internal/tfdiags"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

func dataResourceSchema() providers.Schema {
	return providers.Schema{
		Block: &configschema.Block{
			Attributes: map[string]*configschema.Attribute{
				"input":   {Type: cty.DynamicPseudoType, Optional: true},
				"output":  {Type: cty.DynamicPseudoType, Computed: true},
				"trigger": {Type: cty.DynamicPseudoType, Optional: true},
				"uuid":    {Type: cty.String, Computed: true},
			},
		},
	}
}

func validateDataResourceConfig(req providers.ValidateResourceConfigRequest) (resp providers.ValidateResourceConfigResponse) {
	if req.Config.IsNull() {
		return resp
	}

	// Core does not currently validate computed values are not set in the
	// configuration.
	for _, attr := range []string{"uuid", "output"} {
		if !req.Config.GetAttr(attr).IsNull() {
			resp.Diagnostics = resp.Diagnostics.Append(fmt.Errorf(`%q attribute is read-only`, attr))
		}
	}
	return resp
}

func upgradeDataResourceState(req providers.UpgradeResourceStateRequest) (resp providers.UpgradeResourceStateResponse) {
	ty := dataResourceSchema().Block.ImpliedType()
	val, err := ctyjson.Unmarshal(req.RawStateJSON, ty)
	if err != nil {
		resp.Diagnostics = resp.Diagnostics.Append(err)
		return resp
	}

	resp.UpgradedState = val
	return resp
}

func readDataResourceState(req providers.ReadResourceRequest) (resp providers.ReadResourceResponse) {
	resp.NewState = req.PriorState
	return resp
}

func planDataResourceChange(req providers.PlanResourceChangeRequest) (resp providers.PlanResourceChangeResponse) {
	if req.ProposedNewState.IsNull() {
		// destroy op
		resp.PlannedState = req.ProposedNewState
		return resp
	}

	planned := req.ProposedNewState.AsValueMap()

	input := req.ProposedNewState.GetAttr("input")
	trigger := req.ProposedNewState.GetAttr("trigger")

	switch {
	case req.PriorState.IsNull():
		// Create
		// Set the uuid value to unknown.
		planned["uuid"] = cty.UnknownVal(cty.String)

		// Only compute a new output if input has a non-null value.
		if !input.IsNull() {
			planned["output"] = cty.UnknownVal(input.Type())
		}

		resp.PlannedState = cty.ObjectVal(planned)
		return resp

	case !req.PriorState.GetAttr("trigger").RawEquals(trigger):
		// trigger changed, so we need to replace the entire instance
		resp.RequiresReplace = append(resp.RequiresReplace, cty.GetAttrPath("trigger"))
		planned["uuid"] = cty.UnknownVal(cty.String)

		// We need to check the input for the replacement instance to compute a
		// new output.
		if input.IsNull() {
			planned["output"] = cty.NullVal(cty.DynamicPseudoType)
		} else {
			planned["output"] = cty.UnknownVal(input.Type())
		}

	case !req.PriorState.GetAttr("input").RawEquals(input):
		// only input changed, so we only need to re-compute output
		planned["output"] = cty.UnknownVal(input.Type())
	}

	resp.PlannedState = cty.ObjectVal(planned)
	return resp
}

var testUUIDHook func() string

func applyDataResourceChange(req providers.ApplyResourceChangeRequest) (resp providers.ApplyResourceChangeResponse) {
	if req.PlannedState.IsNull() {
		resp.NewState = req.PlannedState
		return resp
	}

	newState := req.PlannedState.AsValueMap()

	if !req.PlannedState.GetAttr("output").IsKnown() {
		newState["output"] = req.PlannedState.GetAttr("input")
	}

	if !req.PlannedState.GetAttr("uuid").IsKnown() {
		uuidString, err := uuid.GenerateUUID()
		// Terraform would probably never get this far without a good random
		// source, but catch the error anyway.
		if err != nil {
			diag := tfdiags.AttributeValue(
				tfdiags.Error,
				"Error generating uuid",
				err.Error(),
				cty.GetAttrPath("uuid"),
			)

			resp.Diagnostics = resp.Diagnostics.Append(diag)
		}

		if testUUIDHook != nil {
			uuidString = testUUIDHook()
		}

		newState["uuid"] = cty.StringVal(uuidString)
	}

	resp.NewState = cty.ObjectVal(newState)

	return resp
}