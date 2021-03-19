package planner

import (
	"encoding/base64"
	"fmt"

	rkev1 "github.com/rancher/rancher-operator/pkg/apis/rke.cattle.io/v1"
	"github.com/rancher/rancher-operator/pkg/apis/rke.cattle.io/v1/plan"
)

func (p *Planner) getControlPlaneManifests(cluster *rkev1.RKECluster, runtime string) ([]plan.File, error) {
	// NOTE: The agent does not have a means to delete files.  If you add a manifest that
	// may not exist in the future then you should create an empty file to "delete" the file

	clusterAgent, err := p.getClusterAgent(cluster, runtime)
	if err != nil {
		return nil, err
	}

	return []plan.File{
		clusterAgent,
	}, nil
}

func (p *Planner) getClusterAgent(cluster *rkev1.RKECluster, runtime string) (plan.File, error) {
	data, err := p.loadClusterAgent(cluster)
	if err != nil {
		return plan.File{}, err
	}

	return plan.File{
		Content: base64.StdEncoding.EncodeToString(data),
		Path:    fmt.Sprintf("/var/lib/rancher/%s/server/manifests/cluster-agent.yaml", runtime),
	}, nil
}
