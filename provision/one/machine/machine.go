package machine

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	nsqp "github.com/crackcomm/nsqueue/producer"
	"github.com/megamsys/libgo/events"
	"github.com/megamsys/libgo/events/alerts"
	"github.com/megamsys/libgo/utils"
	constants "github.com/megamsys/libgo/utils"
	"github.com/megamsys/opennebula-go/compute"
	"github.com/megamsys/opennebula-go/virtualmachine"
	"github.com/megamsys/vertice/carton"
	lb "github.com/megamsys/vertice/logbox"
	"github.com/megamsys/vertice/meta"
	"github.com/megamsys/vertice/provision"
	"github.com/megamsys/vertice/provision/one/cluster"
)

const (
	ACCOUNTID    = "AccountId"
	ASSEMBLYID   = "AssemblyId"
	ASSEMBLYNAME = "AssemblyName"
	CONSUMED     = "Consumed"
	STARTTIME    = "StartTime"
	ENDTIME      = "EndTime"
)

type OneProvisioner interface {
	Cluster() *cluster.Cluster
}

type Machine struct {
	Name       string
	Id         string
	CartonId   string
	AccountsId string
	Level      provision.BoxLevel
	SSH        provision.BoxSSH
	Image      string
	VCPUThrottle string
	VMId        string
	VNCHost      string
	VNCPort      string
	Routable   bool
	Status     utils.Status
}

type CreateArgs struct {
	Commands    []string
	Box         *provision.Box
	Compute     provision.BoxCompute
	Deploy      bool
	Provisioner OneProvisioner
}


func (m *Machine) Create(args *CreateArgs) error {
	log.Infof("  creating machine in one (%s, %s)", m.Name, m.Image)
ThrottleFactor, _  := strconv.Atoi(m.VCPUThrottle)
cpuThrottleFactor :=float64(ThrottleFactor)
res := strconv.FormatInt(int64(args.Box.GetCpushare()), 10)
ICpu, _ := strconv.Atoi(res)
throttle := float64(ICpu)
realCPU :=throttle/cpuThrottleFactor
opts := compute.VirtualMachine{
		Name:   m.Name,
		Image:  m.Image,
		Cpu:     strconv.FormatFloat(realCPU, 'f', 6, 64),//ugly, compute has the info.
		Memory: strconv.FormatInt(int64(args.Box.GetMemory()), 10),
		HDD:    strconv.FormatInt(int64(args.Box.GetHDD()), 10),
		ContextMap: map[string]string{compute.ASSEMBLY_ID: args.Box.CartonId,
			compute.ASSEMBLIES_ID: args.Box.CartonsId},
		}
	//m.addEnvsToContext(m.BoxEnvs, &vm)
 _,	_, vmid, err := args.Provisioner.Cluster().CreateVM(opts)
	if err != nil {
		return err
	}
	m.VMId = vmid

	var id = make(map[string][]string)
		vm := []string{}
		vm = []string{m.VMId}
	 id[carton.VMID] = vm
		if asm, err := carton.NewAmbly(m.CartonId); err != nil {
			return err
		} else if err = asm.NukeAndSetOutputs(id); err != nil {
			return err
		}
	return nil
}

func (m *Machine) VmHostIpPort(args *CreateArgs) error {

	//time.Sleep(time.Second * 25)

  var Wait int
    ch := make(chan int)
		for i := 0; i < 100; i++ {
		    go player(ch)
		}
    ch <- Wait
    time.Sleep(25 * time.Second)
    <-ch

   opts := virtualmachine.Vnc {
		VmId:   m.VMId,
			}
 	vnchost, vncport, err := args.Provisioner.Cluster().GetIpPort(opts)
	if err != nil {
		return err
	}
	m.VNCHost = vnchost
	m.VNCPort = vncport
	return nil
}

func player(ch chan int) {
    for {
        wait := <-ch
        wait++
        time.Sleep(150 * time.Millisecond)
        ch <- wait
    }
}

func (m *Machine) UpdateVncHost() error {

	var vnchost = make(map[string][]string)
		host := []string{}
		host = []string{m.VNCHost}
	 vnchost[carton.VNCHOST] = host
		if asm, err := carton.NewAmbly(m.CartonId); err != nil {
			return err
		} else if err = asm.NukeAndSetOutputs(vnchost); err != nil {
			return err
		}
		return nil
}

func (m *Machine) UpdateVncPort() error {

	var vncport = make(map[string][]string)
		port := []string{}
		port = []string{m.VNCPort}
	 vncport[carton.VNCPORT] = port
		if asm, err := carton.NewAmbly(m.CartonId); err != nil {
			return err
		} else if err = asm.NukeAndSetOutputs(vncport); err != nil {
			return err
		}
		return nil
}

func (m *Machine) Remove(p OneProvisioner) error {
	log.Debugf("  removing machine in one (%s)", m.Name)
	opts := compute.VirtualMachine{
		Name: m.Name,
	}

	err := p.Cluster().DestroyVM(opts)
	if err != nil {
		return err
	}
	return nil
}

//trigger multi event in the order
func (m *Machine) Deduct() error {
	mi := make(map[string]string)
	mi[ACCOUNTID] = m.AccountsId
	mi[ASSEMBLYID] = m.CartonId
	mi[ASSEMBLYNAME] = m.Name
	mi[CONSUMED] = "0.1"
	mi[STARTTIME] = time.Now().String()
	mi[ENDTIME] = time.Now().String()

	newEvent := events.NewMulti(
		[]*events.Event{
			&events.Event{
				AccountsId:  m.AccountsId,
				EventAction: alerts.DEDUCT,
				EventType:   constants.EventBill,
				EventData:   alerts.EventData{M: mi},
				Timestamp:   time.Now().Local(),
			},
			&events.Event{
				AccountsId:  m.AccountsId,
				EventAction: alerts.TRANSACTION, //Change type to transaction
				EventType:   constants.EventBill,
				EventData:   alerts.EventData{M: mi},
				Timestamp:   time.Now().Local(),
			},
		})
	return newEvent.Write()
}

func (m *Machine) LifecycleOps(p OneProvisioner, action string) error {
	log.Debugf("  %s machine in one (%s)", action, m.Name)
	opts := compute.VirtualMachine{
		Name: m.Name,
	}
	err := p.Cluster().VM(opts, action)
	if err != nil {
		return err
	}
	return nil
}

//it possible to have a Notifier interface that does this, duck typed b y Assembly, Components.
func (m *Machine) SetStatus(status utils.Status) error {
	log.Debugf("  set status[%s] of machine (%s, %s)", m.Id, m.Name, status.String())

	if asm, err := carton.NewAmbly(m.CartonId); err != nil {
		return err
	} else if err = asm.SetStatus(status); err != nil {

		return err
	}

	if m.Level == provision.BoxSome {
		log.Debugf("  set status[%s] of machine (%s, %s)", m.Id, m.Name, status.String())

		if comp, err := carton.NewComponent(m.Id); err != nil {
			return err
		} else if err = comp.SetStatus(status); err != nil {
			return err
		}
	}
	return nil
}

//just publish a message stateup to the machine.
func (m *Machine) ChangeState(status utils.Status) error {
	log.Debugf("  change state of machine (%s, %s)", m.Name, status.String())

	pons := nsqp.New()
	if err := pons.Connect(meta.MC.NSQd[0]); err != nil {
		return err
	}

	bytes, err := json.Marshal(
		carton.Requests{
			CatId:     m.CartonId,
			Action:    status.String(),
			Category:  carton.STATE,
			CreatedAt: time.Now().String(),
		})

	if err != nil {
		return err
	}

	log.Debugf("  pub to machine (%s, %s)", m.Name, bytes)

	if err = pons.Publish(m.Name, bytes); err != nil {
		return err
	}

	defer pons.Stop()
	return nil
}

//if there is a file or something to be created, do it here.
func (m *Machine) Logs(p OneProvisioner, w io.Writer) error {

	fmt.Fprintf(w, lb.W(lb.VM_DEPLOY, lb.INFO, fmt.Sprintf("logs nirvana ! machine %s ", m.Name)))
	return nil
}

func (m *Machine) Exec(p OneProvisioner, stdout, stderr io.Writer, cmd string, args ...string) error {
	cmds := []string{"/bin/bash", "-lc", cmd}
	cmds = append(cmds, args...)

	//load the ssh key inmemory
	//ssh and run the command
	//sshOpts := ssh.CreateExecOptions{
	//}

	//if err != nil {
	//	return err
	//}

	return nil

}

func (m *Machine) SetRoutable(ip string) {
	m.Routable = (len(strings.TrimSpace(ip)) > 0)
}

func (m *Machine) addEnvsToContext(envs string, cfg *compute.VirtualMachine) {
	/*
		for _, envData := range envs {
			cfg.Env = append(cfg.Env, fmt.Sprintf("%s=%s", envData.Name, envData.Value))
		}
			cfg.Env = append(cfg.Env, []string{
				fmt.Sprintf("%s=%s", "MEGAM_HOST", host),
			}...)
	*/
}
