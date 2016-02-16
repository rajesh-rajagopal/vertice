/*
** Copyright [2013-2016] [Megam Systems]
**
** Licensed under the Apache License, Version 2.0 (the "License");
** you may not use this file except in compliance with the License.
** You may obtain a copy of the License at
**
** http://www.apache.org/licenses/LICENSE-2.0
**
** Unless required by applicable law or agreed to in writing, software
** distributed under the License is distributed on an "AS IS" BASIS,
** WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
** See the License for the specific language governing permissions and
** limitations under the License.
 */

package one

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/megamsys/libgo/action"
	"github.com/megamsys/libgo/cmd"
	"github.com/megamsys/opennebula-go/api"
	"github.com/megamsys/vertice/events"
	"github.com/megamsys/vertice/events/alerts"
	"github.com/megamsys/vertice/provision"
	"github.com/megamsys/vertice/provision/one/cluster"
	"github.com/megamsys/vertice/repository"
	"github.com/megamsys/vertice/router"
	_ "github.com/megamsys/vertice/router/route53"
)

var mainOneProvisioner *oneProvisioner

func init() {
	mainOneProvisioner = &oneProvisioner{}
	provision.Register("one", mainOneProvisioner)
}

type oneProvisioner struct {
	defaultImage string
	cluster      *cluster.Cluster
	storage      cluster.Storage
}

func (p *oneProvisioner) Cluster() *cluster.Cluster {
	if p.cluster == nil {
		panic("✗ one cluster")
	}
	return p.cluster
}

func (p *oneProvisioner) String() string {
	if p.cluster == nil {
		return "✗ one cluster"
	}
	return "ready"
}

func (p *oneProvisioner) Initialize(m map[string]string, b map[string]string) error {
	return p.initOneCluster(m)
}

func (p *oneProvisioner) initOneCluster(m map[string]string) error {
	var err error
	if p.storage == nil {
		p.storage, err = buildClusterStorage()
		if err != nil {
			return err
		}
	}
	p.defaultImage = m[api.IMAGE]
	var nodes []cluster.Node = []cluster.Node{cluster.Node{
		Address:  m[api.ENDPOINT],
		Metadata: m,
	},
	}
	//register nodes using the map.
	p.cluster, err = cluster.New(p.storage, nodes...)
	if err != nil {
		return err
	}
	return nil
}

func buildClusterStorage() (cluster.Storage, error) {
	return &cluster.MapStorage{}, nil
}

func getRouterForBox(box *provision.Box) (router.Router, error) {
	routerName, err := box.GetRouter()
	if err != nil {
		return nil, err
	}
	return router.Get(routerName)
}

func (p *oneProvisioner) StartupMessage() (string, error) {
	w := new(tabwriter.Writer)
	var b bytes.Buffer
	w.Init(&b, 0, 8, 0, '\t', 0)
	b.Write([]byte(cmd.Colorfy("  > one ", "white", "", "bold") + "\t" +
		cmd.Colorfy(p.String(), "cyan", "", "")))
	fmt.Fprintln(w)
	w.Flush()
	return strings.TrimSpace(b.String()), nil
}

func (p *oneProvisioner) GitDeploy(box *provision.Box, w io.Writer) (string, error) {
	imageId, err := p.gitDeploy(box.Repo, box.ImageVersion, w)
	if err != nil {
		return "", err
	}

	return p.deployPipeline(box, imageId, w)
}

func (p *oneProvisioner) gitDeploy(re *repository.Repo, version string, w io.Writer) (string, error) {
	fmt.Fprintf(w, "--- git deploy for box (git:%s)\n", re.Source)
	return p.getBuildImage(re, version), nil
}

func (p *oneProvisioner) ImageDeploy(box *provision.Box, imageId string, w io.Writer) (string, error) {
	fmt.Fprintf(w, "--- image deploy for box (%s, image:%s)\n", box.GetFullName(), imageId)
	isValid, err := isValidBoxImage(box.GetFullName(), imageId)
	if err != nil {
		return "", err
	}

	if !isValid {
		imageId = p.getBuildImage(box.Repo, box.ImageVersion)
	}
	return p.deployPipeline(box, imageId, w)
}

//start by validating the image.
//1. &updateStatus in Riak - Deploying..
//2. &create an inmemory machine type from a Box.
//3. &updateStatus in Riak - Creating..
//4. &followLogs by posting it in the queue.
func (p *oneProvisioner) deployPipeline(box *provision.Box, imageId string, w io.Writer) (string, error) {
	fmt.Fprintf(w, "--- deploy box (%s, image:%s)\n", box.GetFullName(), imageId)
	actions := []*action.Action{
		&updateStatusInRiak,
		&createMachine,
		&updateStatusInRiak,
		&deductCons,
		&followLogs,
	}
	pipeline := action.NewPipeline(actions...)

	args := runMachineActionsArgs{
		box:           box,
		imageId:       imageId,
		writer:        w,
		isDeploy:      true,
		machineStatus: provision.StatusLaunching,
		provisioner:   p,
	}

	err := pipeline.Execute(args)
	if err != nil {
		fmt.Fprintf(w, "--- deploy pipeline for box (%s, image:%s)\n --> %s", box.GetFullName(), imageId, err)
		return "", err
	}
	fmt.Fprintf(w, "--- deploy box (%s, image:%s) OK\n", box.GetFullName(), imageId)
	return imageId, nil
}

func (p *oneProvisioner) Destroy(box *provision.Box, w io.Writer) error {
	fmt.Fprintf(w, "\n--- destroying box (%s)\n", box.GetFullName())
	args := runMachineActionsArgs{
		box:           box,
		writer:        w,
		isDeploy:      false,
		machineStatus: provision.StatusDestroying,
		provisioner:   p,
	}

	actions := []*action.Action{
		&updateStatusInRiak,
		&destroyOldMachine,
		&destroyOldRoute,
	}

	pipeline := action.NewPipeline(actions...)

	err := pipeline.Execute(args)
	if err != nil {
		fmt.Fprintf(w, "--- destroying box (%s)\n --> %s", box.GetFullName(), err)
		return err
	}
	fmt.Fprintf(w, "\n--- destroying box (%s) OK\n", box.GetFullName())
	err = doneNotify(box, w, alerts.DESTROYED)
	return nil
}

func (p *oneProvisioner) SetState(box *provision.Box, w io.Writer, changeto provision.Status) error {
	fmt.Fprintf(w, "\n--- stateto %s\n", box.GetFullName())
	args := runMachineActionsArgs{
		box:           box,
		writer:        w,
		machineStatus: changeto,
		provisioner:   p,
	}

	actions := []*action.Action{
		&changeStateofMachine,
		&addNewRoute,
	}

	pipeline := action.NewPipeline(actions...)

	err := pipeline.Execute(args)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "\n--- stateto %s OK\n", box.GetFullName())
	err = doneNotify(box, w, alerts.LAUNCHED)
	return err
}

func (p *oneProvisioner) Restart(box *provision.Box, process string, w io.Writer) error {
	fmt.Fprintf(w, "\n--- restarting box (%s)\n", box.GetFullName())
	args := runMachineActionsArgs{
		box:           box,
		writer:        w,
		isDeploy:      false,
		machineStatus: provision.StatusBootstrapped,
		provisioner:   p,
	}

	actions := []*action.Action{
		&updateStatusInRiak,
		&restartMachine,
		&updateStatusInRiak,
	}

	pipeline := action.NewPipeline(actions...)

	err := pipeline.Execute(args)
	if err != nil {
		fmt.Fprintf(w, "--- restarting box (%s)\n --> %s", box.GetFullName(), err)
		return err
	}
	fmt.Fprintf(w, "\n--- restarting box (%s) OK\n", box.GetFullName())
	return nil
}

func (p *oneProvisioner) Start(box *provision.Box, process string, w io.Writer) error {
	fmt.Fprintf(w, "\n--- starting box (%s)\n", box.GetFullName())
	args := runMachineActionsArgs{
		box:           box,
		writer:        w,
		isDeploy:      false,
		machineStatus: provision.StatusStarting,
		provisioner:   p,
	}

	actions := []*action.Action{
		&updateStatusInRiak,
		&startMachine,
		&updateStatusInRiak,
	}

	pipeline := action.NewPipeline(actions...)

	err := pipeline.Execute(args)
	if err != nil {
		fmt.Fprintf(w, "--- starting box (%s)\n --> %s", box.GetFullName(), err)
		return err
	}
	fmt.Fprintf(w, "\n--- starting box (%s) OK\n", box.GetFullName())
	return nil
}

func (p *oneProvisioner) Stop(box *provision.Box, process string, w io.Writer) error {
	fmt.Fprintf(w, "\n--- stopping box (%s)\n", box.GetFullName())
	args := runMachineActionsArgs{
		box:           box,
		writer:        w,
		isDeploy:      false,
		machineStatus: provision.StatusStopping,
		provisioner:   p,
	}
	actions := []*action.Action{
		&updateStatusInRiak,
		&stopMachine,
		&updateStatusInRiak,
	}

	pipeline := action.NewPipeline(actions...)

	err := pipeline.Execute(args)
	if err != nil {
		fmt.Fprintf(w, "--- stopping box (%s)\n --> %s", box.GetFullName(), err)
		return err
	}
	fmt.Fprintf(w, "\n--- stopping box (%s) OK\n", box.GetFullName())
	return nil
}

func (p *oneProvisioner) Shell(provision.ShellOptions) error {
	return provision.ErrNotImplemented
}

func (*oneProvisioner) Addr(box *provision.Box) (string, error) {
	r, err := getRouterForBox(box)
	if err != nil {
		log.Errorf("Failed to get router: %s", err)
		return "", err
	}
	addr, err := r.Addr(box.GetFullName())
	if err != nil {
		log.Errorf("Failed to obtain box %s address: %s", box.GetFullName(), err)
		return "", err
	}
	return addr, nil
}

func (p *oneProvisioner) MetricEnvs(start int64, end int64, w io.Writer) ([]interface{}, error) {
	fmt.Fprintf(w, "--- pull metrics for the duration (%d, %d)\n", start, end)
	res, err := p.Cluster().Showback(start, end)
	if err != nil {
		fmt.Fprintf(w, "--- pull metrics for the duration  err (%d, %d)\n --> %s", start, end, err)
		return nil, err
	}
	fmt.Fprintf(w, "--- pull metrics for the duration (%d, %d) OK\n", start, end)
	return res, nil
}

func (p *oneProvisioner) SetBoxStatus(box *provision.Box, w io.Writer, status provision.Status) error {
	fmt.Fprintf(w, "\n--- status %s box %s\n", box.GetFullName(), status.String())
	actions := []*action.Action{
		&updateStatusInRiak,
	}
	pipeline := action.NewPipeline(actions...)

	args := runMachineActionsArgs{
		box:           box,
		writer:        w,
		machineStatus: status,
		provisioner:   p,
	}

	err := pipeline.Execute(args)
	if err != nil {
		log.Errorf("error on execute status pipeline for box %s - %s", box.GetFullName(), err)
		return err
	}
	fmt.Fprintf(w, "\n--- status %s box %s OK\n", box.GetFullName(), status.String())
	return nil
}

func (p *oneProvisioner) SetCName(box *provision.Box, cname string) error {
	r, err := getRouterForBox(box)
	if err != nil {
		return err
	}
	return r.SetCName(cname, box.GetFullName())
}

func (p *oneProvisioner) UnsetCName(box *provision.Box, cname string) error {
	r, err := getRouterForBox(box)
	if err != nil {
		return err
	}
	return r.UnsetCName(cname, box.GetFullName())
}

// PlatformAdd build and push a new template into one
func (p *oneProvisioner) PlatformAdd(name string, args map[string]string, w io.Writer) error {
	return nil
}

func (p *oneProvisioner) PlatformUpdate(name string, args map[string]string, w io.Writer) error {
	return p.PlatformAdd(name, args, w)
}

func (p *oneProvisioner) PlatformRemove(name string) error {
	return nil
}

// getBuildImage returns the image name from box or tosca.
func (p *oneProvisioner) getBuildImage(re *repository.Repo, version string) string {
	if p.usePlatformImage(re) {
		return p.defaultImage
	}
	return re.Gitr() //return the url
}

func (p *oneProvisioner) usePlatformImage(re *repository.Repo) bool {
	return !re.OneClick
}

func (p *oneProvisioner) ExecuteCommandOnce(stdout, stderr io.Writer, box *provision.Box, cmd string, args ...string) error {
	/*if boxs, err := p.listRunnableMachinesByBox(box.GetName()); err ! =nil {
					return err
	    }

		if err := nil; err != nil {
			return err
		}
		if len(boxs) == 0 {
			return provision.ErrBoxNotFound
		}
		box := boxs[0]
		return box.Exec(p, stdout, stderr, cmd, args...)
	*/
	return nil
}

func doneNotify(box *provision.Box, w io.Writer, evtAction alerts.EventAction) error {
	fmt.Fprintf(w, "\n--- done %s box \n", box.GetFullName())
	mi := make(map[string]string)
	mi[alerts.VERTNAME] = box.GetFullName()
	mi[alerts.VERTTYPE] = box.Tosca
	newEvent := events.NewMulti(
		[]*events.Event{
			&events.Event{
				AccountsId:  box.AccountsId,
				EventAction: evtAction,
				EventType:   events.EventUser,
				EventData:   events.EventData{M: mi},
				Timestamp:   time.Now().Local(),
			},
		})
	fmt.Fprintf(w, "\n--- done %s box OK\n", box.GetFullName())
	return newEvent.Write()
}
