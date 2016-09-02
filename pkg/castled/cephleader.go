package castled

import (
	"fmt"
	"log"
	"path"

	ctx "golang.org/x/net/context"

	etcd "github.com/coreos/etcd/client"
	"github.com/quantum/castle/pkg/cephd"
	"github.com/quantum/clusterd/pkg/orchestrator"
	"github.com/quantum/clusterd/pkg/util"
)

// Interface implemented by a service that has been elected leader
type cephLeader struct {
	cluster *clusterInfo
}

// Load the state of the service from etcd. Typically a service will populate the desired/discovered state and the applied state
// from etcd, then compute the difference and cache it.
// Returns whether the service has updates to be applied.
func (c *cephLeader) LoadClusterServiceState(context *orchestrator.ClusterContext) (bool, error) {

	return true, nil
}

// Apply the desired state to the cluster. The context provides all the information needed to make changes to the service.
func (c *cephLeader) ConfigureClusterService(context *orchestrator.ClusterContext) error {

	// Create or get the basic cluster info
	var err error
	c.cluster, err = createOrGetClusterInfo(context.EtcdClient)
	if err != nil {
		return err
	}

	// Select the monitors, instruct them to start, and wait for quorum
	err = createMonitors(context, c.cluster)
	if err != nil {
		return err
	}

	// Configure the OSDs
	err = configureOSDs(context)
	if err != nil {
		return err
	}

	return nil
}

func createOrGetClusterInfo(etcdClient etcd.KeysAPI) (*clusterInfo, error) {
	// load any existing cluster info that may have previously been created
	cluster, err := loadClusterInfo(etcdClient)
	if err != nil {
		return nil, fmt.Errorf("failed to load cluster info: %+v", err)
	}

	if cluster == nil {
		// the cluster info is not yet set, go ahead and set it now
		cluster, err = createClusterInfo()
		if err != nil {
			return nil, fmt.Errorf("failed to create cluster info: %+v", err)
		}

		log.Printf("Created new cluster info: %+v", cluster)
		err = saveClusterInfo(cluster, etcdClient)
		if err != nil {
			return nil, fmt.Errorf("failed to save new cluster info: %+v", err)
		}
	} else {
		// the cluster has already been created
		log.Printf("Cluster already exists: %+v", cluster)
	}

	return cluster, nil
}

// attempt to load any previously created and saved cluster info
func loadClusterInfo(etcdClient etcd.KeysAPI) (*clusterInfo, error) {
	resp, err := etcdClient.Get(ctx.Background(), path.Join(cephKey, "fsid"), nil)
	if err != nil {
		if util.IsEtcdKeyNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	fsid := resp.Node.Value

	resp, err = etcdClient.Get(ctx.Background(), path.Join(cephKey, "name"), nil)
	if err != nil {
		return nil, err
	}
	name := resp.Node.Value

	secretsKey := path.Join(cephKey, "_secrets")

	resp, err = etcdClient.Get(ctx.Background(), path.Join(secretsKey, "monitor"), nil)
	if err != nil {
		return nil, err
	}
	monSecret := resp.Node.Value

	resp, err = etcdClient.Get(ctx.Background(), path.Join(secretsKey, "admin"), nil)
	if err != nil {
		return nil, err
	}
	adminSecret := resp.Node.Value

	cluster := &clusterInfo{
		FSID:          fsid,
		MonitorSecret: monSecret,
		AdminSecret:   adminSecret,
		Name:          name,
	}

	// Get the monitors that have been applied in a previous orchestration
	cluster.Monitors, err = getChosenMonitors(etcdClient)

	return cluster, nil
}

// create new cluster info (FSID, shared keys)
func createClusterInfo() (*clusterInfo, error) {
	fsid, err := cephd.NewFsid()
	if err != nil {
		return nil, err
	}

	monSecret, err := cephd.NewSecretKey()
	if err != nil {
		return nil, err
	}

	adminSecret, err := cephd.NewSecretKey()
	if err != nil {
		return nil, err
	}

	return &clusterInfo{
		FSID:          fsid,
		MonitorSecret: monSecret,
		AdminSecret:   adminSecret,
		Name:          "castlecluster",
	}, nil
}

// save the given cluster info to the key value store
func saveClusterInfo(c *clusterInfo, etcdClient etcd.KeysAPI) error {
	_, err := etcdClient.Set(ctx.Background(), path.Join(cephKey, "fsid"), c.FSID, nil)
	if err != nil {
		return err
	}

	_, err = etcdClient.Set(ctx.Background(), path.Join(cephKey, "name"), c.Name, nil)
	if err != nil {
		return err
	}

	secretsKey := path.Join(cephKey, "_secrets")

	_, err = etcdClient.Set(ctx.Background(), path.Join(secretsKey, "monitor"), c.MonitorSecret, nil)
	if err != nil {
		return err
	}

	_, err = etcdClient.Set(ctx.Background(), path.Join(secretsKey, "admin"), c.AdminSecret, nil)
	if err != nil {
		return err
	}

	return nil
}