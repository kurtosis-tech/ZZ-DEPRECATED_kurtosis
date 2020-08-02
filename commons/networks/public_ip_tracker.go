package networks

import (
	"encoding/binary"
	"github.com/palantir/stacktrace"
	"github.com/sirupsen/logrus"
	"math/big"
	"net"
	"strings"
)

type FreeIpAddrTracker struct {
	log *logrus.Logger
	subnet *net.IPNet
	takenIps map[string]bool
	isIpv4 bool
}

/*
Creates a new tracker that will dole out free IP addresses from the given subnet, making sure not to dole out any IPs
from the list of already-taken IPs
 */
func NewFreeIpAddrTracker(log *logrus.Logger, subnetMask string, alreadyTakenIps []string) (ipAddrTracker *FreeIpAddrTracker, err error) {
	ip, ipv4Net, err := net.ParseCIDR(subnetMask)
	subnetIsIpv4 := isIpv4(ip)
	if err != nil {
		return nil, stacktrace.Propagate(err, "Failed to parse subnet %s as CIDR.", subnetMask)
	}
	takenIps := map[string]bool{}

	for _, ipAddr := range alreadyTakenIps {
		takenIps[ipAddr] = true
	}

	ipAddrTracker = &FreeIpAddrTracker{
		log: log,
		subnet: ipv4Net,
		takenIps: takenIps,
		isIpv4: subnetIsIpv4,
	}
	return ipAddrTracker, nil
}

// TODO rework this entire function to handle IPv6 as well (currently breaks on IPv6)
func (networkManager FreeIpAddrTracker) GetFreeIpAddr() (ipAddr net.IP, err error){

	subnetIp := networkManager.subnet.IP

	// The IP can be either 4 bytes or 16 bytes long; we need to handle both!
	// See https://gist.github.com/ammario/649d4c0da650162efd404af23e25b86b
	if isIpv4(subnetIp) {
		// convert IPNet struct mask and address to uint32
		// network is BigEndian
		mask := binary.BigEndian.Uint32(networkManager.subnet.Mask)
		var intIp uint32
		if len(subnetIp) == 16 {
			intIp = binary.BigEndian.Uint32(subnetIp[12:16])
		} else {
			intIp = binary.BigEndian.Uint32(subnetIp)
		}
		// We remove the zeroth IP because it's only used for specifying the network itself
		start := intIp + 1

		// find the final address
		finish := (start & mask) | (mask ^ 0xffffffff)
		// loop through addresses as uint32
		for i := start; i <= finish; i++ {
			// convert back to net.IP
			ip := make(net.IP, 4)
			binary.BigEndian.PutUint32(ip, i)
			ipStr := ip.String()
			if !networkManager.takenIps[ipStr] {
				networkManager.takenIps[ipStr] = true
				return ip, nil
			}
		}
	} else {
		mask := networkManager.subnet.Mask
		intIp := ipv6ToInt(subnetIp)
		start := intIp.Add(intIp,big.NewInt(1))
	}
	return nil, stacktrace.NewError("Failed to allocate IpAddr on subnet %v - all taken.", networkManager.subnet)
}

/*
	Determining is an IP is v4 or v6 is complicated.
	This logic is taken from https://stackoverflow.com/questions/22751035/golang-distinguish-ipv4-ipv6,
	and seems to be the most robust way to distinguish between IP formats.
 */

func isIpv4(address net.IP) bool {
	return strings.Count(address.String(), ":") < 2
}

/*
	From: https://gist.github.com/ammario/649d4c0da650162efd404af23e25b86b
*/

func ipv6ToInt(IPv6Addr net.IP) *big.Int {
	IPv6Int := big.NewInt(0)
	IPv6Int.SetBytes(IPv6Addr)
	return IPv6Int
}

func IntToIpv6(intipv6 *big.Int) net.IP {
	return intipv6.Bytes()
}

