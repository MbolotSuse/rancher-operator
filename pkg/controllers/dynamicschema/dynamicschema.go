package dynamicschema

import (
	"context"
	"strings"

	rancherv1 "github.com/rancher/rancher-operator/pkg/apis/rancher.cattle.io/v1"
	rkev1 "github.com/rancher/rancher-operator/pkg/apis/rke.cattle.io/v1"
	"github.com/rancher/rancher-operator/pkg/clients"
	mgmtcontrollers "github.com/rancher/rancher-operator/pkg/generated/controllers/management.cattle.io/v3"
	v3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	"github.com/rancher/wrangler/pkg/crd"
	"github.com/rancher/wrangler/pkg/data/convert"
	"github.com/rancher/wrangler/pkg/generic"
	"github.com/rancher/wrangler/pkg/schemas"
	"github.com/rancher/wrangler/pkg/schemas/openapi"
	apierror "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	nodeAPIGroup = "rke-node.cattle.io"
)

type handler struct {
	schemaCache mgmtcontrollers.DynamicSchemaCache
}

func Register(ctx context.Context, clients *clients.Clients) {
	h := handler{
		schemaCache: clients.Management.DynamicSchema().Cache(),
	}
	mgmtcontrollers.RegisterDynamicSchemaGeneratingHandler(ctx,
		clients.Management.DynamicSchema(),
		clients.Apply.WithCacheTypes(clients.CRD.CustomResourceDefinition()),
		"",
		"dynamic-driver-crd",
		h.OnChange,
		&generic.GeneratingHandlerOptions{
			AllowClusterScoped: true,
		})
}

func getStatusSchema(allSchemas *schemas.Schemas) (*schemas.Schema, error) {
	return allSchemas.Import(rkev1.RKEMachineStatus{})
}

func getSchemas(name string, spec *v3.DynamicSchemaSpec) (string, string, string, *schemas.Schemas, error) {
	var (
		nodeConfigID = name + "Config"
		machineID    = name + "Machine"
		templateID   = name + "MachineTemplate"
	)

	allSchemas, err := schemas.NewSchemas()
	if err != nil {
		return "", "", "", nil, err
	}

	specSchema, err := getSpecSchemas(name, allSchemas, spec)
	if err != nil {
		return "", "", "", nil, err
	}

	statusSchema, err := getStatusSchema(allSchemas)
	if err != nil {
		return "", "", "", nil, err
	}

	baseSchema := schemas.Schema{
		ResourceFields: map[string]schemas.Field{
			"spec": {
				Type: specSchema.ID,
			},
			"status": {
				Type: statusSchema.ID,
			},
		},
	}

	for _, id := range []string{machineID, templateID} {
		baseSchema.ID = id
		if err := allSchemas.AddSchema(baseSchema); err != nil {
			return "", "", "", nil, err
		}
	}

	specSchema.ID = nodeConfigID
	delete(specSchema.ResourceFields, "common")
	if err := allSchemas.AddSchema(*specSchema); err != nil {
		return "", "", "", nil, err
	}

	return nodeConfigID, templateID, machineID, allSchemas, nil
}

func getSpecSchemas(name string, allSchemas *schemas.Schemas, spec *v3.DynamicSchemaSpec) (*schemas.Schema, error) {
	specSchema := schemas.Schema{}
	if err := convert.ToObj(spec, &specSchema); err != nil {
		return nil, err
	}
	specSchema.ID = name + "Spec"

	commonField, err := allSchemas.Import(rkev1.RKECommonNodeConfig{})
	if err != nil {
		return nil, err
	}

	if specSchema.ResourceFields == nil {
		specSchema.ResourceFields = map[string]schemas.Field{}
	}

	specSchema.ResourceFields["common"] = schemas.Field{
		Type: commonField.ID,
	}

	if err := allSchemas.AddSchema(specSchema); err != nil {
		return nil, err
	}

	return allSchemas.Schema(specSchema.ID), nil
}

func (h *handler) OnChange(obj *v3.DynamicSchema, status v3.DynamicSchemaStatus) ([]runtime.Object, v3.DynamicSchemaStatus, error) {
	name, node, _, err := h.getStyle(obj.Name)
	if err != nil {
		return nil, status, err
	}

	if !node { // only support nodes right now  && !cluster {
		return nil, status, nil
	}

	nodeConfigID, templateID, machineID, schemas, err := getSchemas(name, &obj.Spec)
	if err != nil {
		return nil, status, err
	}

	var result []runtime.Object

	for _, id := range []string{nodeConfigID, templateID, machineID} {
		props, err := openapi.ToOpenAPI(id, schemas)
		if err != nil {
			return nil, status, err
		}
		crd := crd.CRD{
			GVK: schema.GroupVersionKind{
				Group:   nodeAPIGroup,
				Version: rkev1.SchemeGroupVersion.Version,
				Kind:    convert.Capitalize(id),
			},
			Schema: props,
			Labels: map[string]string{
				"cluster.x-k8s.io/v1alpha4": "v1",
			},
			Status: true,
		}

		if nodeConfigID == id {
			crd.GVK.Group = rancherv1.SchemeGroupVersion.Group
		}

		crdObj, err := crd.ToCustomResourceDefinition()
		if err != nil {
			return nil, status, err
		}
		result = append(result, crdObj)
	}

	return result, status, nil
}

func (h *handler) getStyle(name string) (string, bool, bool, error) {
	if !strings.HasSuffix(name, "config") {
		return "", false, false, nil
	}

	for _, typeName := range []string{"nodetemplateconfig", "cluster"} {
		schema, err := h.schemaCache.Get(typeName)
		if apierror.IsNotFound(err) {
			continue
		} else if err != nil {
			return "", false, false, err
		}
		for key := range schema.Spec.ResourceFields {
			if strings.EqualFold(key, name) {
				return strings.TrimSuffix(key, "Config"),
					typeName == "nodetemplateconfig",
					typeName == "cluster",
					nil
			}
		}
	}

	return "", false, false, nil
}
