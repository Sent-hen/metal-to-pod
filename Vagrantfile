# -*- mode: ruby -*-
# vi: set ft=ruby :
#
# Hypervisor layer: defines the lab VMs on VMware Workstation (primary) or
# VirtualBox (fallback). Topology comes from provision/lab.env, the same file
# the provisioning scripts read, so the network plan lives in exactly one place.
#
#   vagrant up --no-parallel      # order matters: cp1 -> workers -> ci
#
# --no-parallel is required: cp1 creates the CA and the join token that the
# other VMs wait for.

LAB = File.readlines(File.join(__dir__, "provision", "lab.env"))
          .map(&:strip)
          .reject { |l| l.empty? || l.start_with?("#") }
          .to_h { |l| l.split("=", 2) }

def worker_ip(base, n)
  octets = base.split(".").map(&:to_i)
  octets[3] += n
  octets.join(".")
end

worker_count = Integer(ENV.fetch("WORKERS", LAB["WORKER_COUNT"]))
if worker_count != Integer(LAB["WORKER_COUNT"])
  abort "WORKERS=#{worker_count} but provision/lab.env says WORKER_COUNT=#{LAB['WORKER_COUNT']}. " \
        "Edit lab.env instead so /etc/hosts on every VM agrees."
end

NODES = [
  { name: "cp1", ip: LAB["CP_IP"], mem: LAB["CP_MEMORY"], cpus: LAB["CP_CPUS"], role: :control_plane },
  *(1..worker_count).map do |i|
    { name: "worker#{i}", ip: worker_ip(LAB["WORKER_IP_BASE"], i),
      mem: LAB["WORKER_MEMORY"], cpus: LAB["WORKER_CPUS"], role: :worker }
  end,
  { name: "ci", ip: LAB["CI_IP"], mem: LAB["CI_MEMORY"], cpus: LAB["CI_CPUS"], role: :ci },
].freeze

Vagrant.configure("2") do |config|
  # bento boxes are published for both vmware_desktop and virtualbox.
  config.vm.box = ENV.fetch("LAB_BOX", "bento/ubuntu-24.04")
  config.vm.box_check_update = false
  config.vm.synced_folder ".", "/vagrant"

  NODES.each do |node|
    config.vm.define node[:name] do |vm|
      vm.vm.hostname = node[:name]
      vm.vm.network "private_network", ip: node[:ip]

      vm.vm.provider "vmware_desktop" do |v|
        v.gui = false
        v.vmx["memsize"] = node[:mem]
        v.vmx["numvcpus"] = node[:cpus]
      end

      vm.vm.provider "virtualbox" do |vb|
        vb.name = "metal-to-pod-#{node[:name]}"
        vb.memory = node[:mem]
        vb.cpus = node[:cpus]
      end

      vm.vm.provision "common", type: "shell", path: "provision/common.sh"

      case node[:role]
      when :control_plane
        vm.vm.provision "k8s-node", type: "shell", path: "provision/k8s-node.sh", env: { "NODE_IP" => node[:ip] }
        vm.vm.provision "control-plane", type: "shell", path: "provision/control-plane.sh"
      when :worker
        vm.vm.provision "k8s-node", type: "shell", path: "provision/k8s-node.sh", env: { "NODE_IP" => node[:ip] }
        vm.vm.provision "worker", type: "shell", path: "provision/worker.sh"
      when :ci
        vm.vm.provision "ci", type: "shell", path: "provision/ci.sh"
      end
    end
  end
end
