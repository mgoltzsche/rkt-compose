package launcher

import (
	"bufio"
	"fmt"
	"github.com/mgoltzsche/log"
	"github.com/mgoltzsche/model"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var toIdRegexp = regexp.MustCompile("[^a-z0-9]+")

type Loader struct {
	descriptors          *model.Descriptors
	images               *model.Images
	defaultVolumeBaseDir string
	substitutes          *Substitutes
	debug                log.Logger
}

func NewLoader(descriptors *model.Descriptors, images *model.Images, defaultVolumeBaseDir string, warn, debug log.Logger) *Loader {
	env := map[string]string{}
	_, err := os.Stat(".env")
	if err == nil {
		readEnvFile(".env", env)
	} else if !os.IsNotExist(err) {
		debug.Printf("warn: cannot access .env file: %s", err)
	}
	for _, e := range os.Environ() {
		s := strings.SplitN(e, "=", 2)
		env[s[0]] = s[1]
	}
	return &Loader{descriptors, images, defaultVolumeBaseDir, NewSubstitutes(env, warn), debug}
}

func (self *Loader) LoadPod(d *model.PodDescriptor) (pod *Pod, err error) {
	pod = &Pod{}
	pod.File = d.File
	pod.Name = self.effectiveString(d.Name)
	hostname := self.effectiveString(d.Hostname)
	domainname := self.effectiveString(d.Domainname)
	if hostname == "" {
		hostname = pod.Name
	}
	dotPos := strings.Index(hostname, ".")
	if dotPos != -1 {
		domainname = hostname[dotPos+1:]
		hostname = hostname[:dotPos]
	}
	pod.Hostname = hostname
	pod.Domainname = domainname
	pod.Net = self.effectiveStringArray(d.Net)
	pod.Dns = self.effectiveStringArray(d.Dns)
	pod.DnsSearch = self.effectiveStringArray(d.DnsSearch)
	pod.DisableHostsInjection, err = self.effectiveBool(d.DisableHostsInjection)
	if err != nil {
		return
	}
	pod.Environment = self.effectiveStringMap(d.Environment)
	pod.Services, err = self.toServices(d)
	if err != nil {
		return
	}
	pod.Volumes, err = self.toVolumes(d)
	if err != nil {
		return
	}
	pod.SharedKeys = self.effectiveStringMap(d.SharedKeys)
	pod.SharedKeysOverrideAllowed, err = self.effectiveBool(d.SharedKeysOverrideAllowed)
	if err != nil {
		return
	}
	pod.StopGracePeriod, err = self.effectiveDuration(d.StopGracePeriod, "10s")
	if err != nil {
		return
	}
	self.fileMountsToVolumes(pod)
	self.addImageVolumes(pod)
	return
}

func (self *Loader) toServices(d *model.PodDescriptor) (map[string]*Service, error) {
	s := map[string]*Service{}
	build := map[string]func() error{}
	for k, v := range d.Services {
		dest := NewService()
		err := self.applyService(v, d, dest, build, map[string]bool{})
		if err != nil {
			return nil, err
		}
		s[k] = dest
	}
	for _, v := range s {
		var img *model.ImageMetadata
		var err error
		if imgBuild, ok := build[v.Image]; ok {
			err = imgBuild()
			if err != nil {
				return nil, err
			}
		}
		img, err = self.images.Image(v.Image)
		if err != nil {
			return nil, err
		}
		// Assign properties derived from image
		if len(v.Entrypoint) == 0 {
			v.Entrypoint = []string{img.Exec[0]}
			if len(v.Command) == 0 {
				v.Command = img.Exec[1:]
			}
		}
	}
	return s, nil
}

func (self *Loader) applyService(s *model.ServiceDescriptor, d *model.PodDescriptor, t *Service, build map[string]func() error, visited map[string]bool) error {
	if s.Extends != nil {
		baseServName := self.effectiveString(s.Extends.Service)
		if baseServName == "" {
			return fmt.Errorf(".extend.service is empty")
		}
		basePod := d
		if s.Extends.File != "" {
			var err error
			basePod, err = self.descriptors.Descriptor(absPath(self.effectiveString(s.Extends.File), d.File))
			if err != nil {
				return fmt.Errorf("extends: %s", err)
			}
		}
		baseServ := basePod.Services[baseServName]
		if baseServ == nil {
			return fmt.Errorf("Extended service %q does not exist", baseServName)
		}
		baseKey := basePod.File + "/" + baseServName
		if _, ok := visited[baseKey]; ok {
			i := 0
			keys := make([]string, len(visited))
			for k := range visited {
				keys[i] = k
				i++
			}
			return fmt.Errorf("circular extension: %s", keys)
		}
		visited[baseKey] = true
		if err := self.applyService(baseServ, basePod, t, build, visited); err != nil {
			return err
		}
	}
	if s.Image != "" {
		t.Image = self.effectiveString(s.Image)
	}
	if t.Image == "" && s.Build == nil {
		return fmt.Errorf("service has no image")
	}
	if s.Build != nil {
		buildCtx := absPath(self.effectiveString(s.Build.Context), d.File)
		df := self.effectiveString(s.Build.Dockerfile)
		if df == "" {
			df = "Dockerfile"
		}
		df = path.Clean(absPath(df, buildCtx+"/"))
		if t.Image == "" {
			imgName, err := self.generateImageName(df)
			if err != nil {
				return err
			}
			t.Image = imgName
		}
		build[t.Image] = func() error {
			_, err := self.images.BuildImage(t.Image, df, buildCtx)
			return err
		}
	}
	if len(s.Entrypoint) > 0 {
		t.Entrypoint = self.effectiveStringArray(s.Entrypoint)
	}
	if len(s.Command) > 0 {
		t.Command = self.effectiveStringArray(s.Command)
	}
	err := self.toEnvironment(s, d.File, t.Environment)
	if err != nil {
		return err
	}
	t.Ports, err = self.toPorts(s.Ports, t.Ports)
	if err != nil {
		return err
	}
	for k, v := range self.effectiveStringMap(s.Mounts) {
		t.Mounts[absPath(k, "/")] = absPath(v, d.File)
	}
	if err = self.toHealthCheck(s.HealthCheck, t.HealthCheck); err != nil {
		return err
	}
	return nil
}

func (self *Loader) generateImageName(df string) (string, error) {
	st, err := os.Stat(df)
	if err != nil {
		return "", fmt.Errorf("cannot access dockerfile %q: %s", df, err)
	}
	tag := st.ModTime().Format("20060102150405")
	return "local/" + toId(df) + ":" + tag, nil
}

func (self *Loader) toEnvironment(s *model.ServiceDescriptor, podFile string, e map[string]string) (err error) {
	for _, f := range s.EnvFile {
		err = readEnvFile(absPath(f, podFile), e)
		if err != nil {
			return
		}
	}
	for k, v := range s.Environment {
		e[k] = self.effectiveString(v)
	}
	return
}

func readEnvFile(file string, r map[string]string) error {
	f, err := os.Open(filepath.FromSlash(file))
	if err != nil {
		return fmt.Errorf("cannot open env file %q: %s", file, err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	i := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" && strings.Index(line, "#") != 0 {
			kv := strings.SplitN(line, "=", 2)
			if len(kv) != 2 {
				return fmt.Errorf("invalid env file entry at %s:%d: %q", file, i, kv)
			}
			r[kv[0]] = kv[1]
		}
		i++
	}
	if err = scanner.Err(); err != nil {
		return fmt.Errorf("cannot read env file %q: %s", file, err)
	}
	return nil
}

func (self *Loader) toPorts(s []*model.PortBindingDescriptor, t []*PortBinding) ([]*PortBinding, error) {
	var err error
	for _, e := range s {
		p := &PortBinding{}
		p.Target, err = self.effectiveUint16(e.Target)
		if err != nil {
			return nil, fmt.Errorf("invalid target port: %s", err)
		}
		p.Published, err = self.effectiveUint16(e.Published)
		if err != nil {
			return nil, fmt.Errorf("invalid published port: %s", err)
		}
		p.IP = self.effectiveString(e.IP)
		p.Protocol = self.effectiveString(e.Protocol)
		for _, ex := range t {
			if ex.Target == p.Target && ex.Target == p.Target {
				ex.IP = p.IP
				ex.Published = p.Published
				return t, nil
			}
		}
		t = append(t, p)
	}
	return t, nil
}

func (self *Loader) toHealthCheck(s *model.HealthCheckDescriptor, t *HealthCheckDescriptor) (err error) {
	if s == nil {
		return nil
	}
	t.Command = self.effectiveStringArray(s.Command)
	t.Http = self.effectiveString(s.Http)
	t.Interval, err = self.effectiveDuration(s.Interval, "30s")
	if err != nil {
		return fmt.Errorf("invalid healthcheck interval: %s", err)
	}
	t.Timeout, err = self.effectiveDuration(s.Timeout, "20s")
	if err != nil {
		return fmt.Errorf("invalid healthcheck timeout: %s", err)
	}
	t.Retries, err = self.effectiveUint(s.Retries)
	if err != nil {
		return fmt.Errorf("invalid healthcheck retries: %s", err)
	}
	t.Disable, err = self.effectiveBool(s.Disable)
	if err != nil {
		return fmt.Errorf("invalid healthcheck disable: %s", err)
	}
	return nil
}

func (self *Loader) toVolumes(d *model.PodDescriptor) (map[string]*Volume, error) {
	r := map[string]*Volume{}
	for k, v := range d.Volumes {
		kind := v.Kind
		if kind == "" {
			kind = "host"
		}
		ro, err := self.effectiveBool(v.Readonly)
		if err != nil {
			return nil, fmt.Errorf("invalid volume readonly value: %s", err)
		}
		r[k] = &Volume{absPath(v.Source, d.File), kind, ro}
	}
	return r, nil
}

func (self *Loader) fileMountsToVolumes(pod *Pod) {
	for _, s := range pod.Services {
		for k, v := range s.Mounts {
			v = self.effectiveString(v)
			if isPath(v) {
				volName := toId(relPath(v, pod.File))
				s.Mounts[k] = volName
				if _, ok := pod.Volumes[volName]; !ok {
					pod.Volumes[volName] = &Volume{v, "host", false}
				}
			}
		}
	}
}

func (self *Loader) addImageVolumes(pod *Pod) error {
	for _, s := range pod.Services {
		img, err := self.images.Image(s.Image)
		if err != nil {
			return err
		}
		for volName, _ := range img.MountPoints {
			if _, ok := pod.Volumes[volName]; !ok {
				src := absPath(self.defaultVolumeBaseDir+"/"+volName, pod.File)
				pod.Volumes[volName] = &Volume{src, "host", false}
			}
		}
	}
	return nil
}

func (self *Loader) effectiveString(v string) string {
	return self.substitutes.Substitute(v)
}

func (self *Loader) effectiveStringArray(a []string) []string {
	r := make([]string, len(a))
	for i, v := range a {
		r[i] = self.effectiveString(v)
	}
	return r
}

func (self *Loader) effectiveStringMap(m map[string]string) map[string]string {
	r := make(map[string]string, len(m))
	for k, v := range m {
		r[k] = self.effectiveString(v)
	}
	return r
}

func (self *Loader) effectiveBool(v model.BoolVal) (bool, error) {
	s := string(v)
	if s == "" {
		return false, nil
	}
	return strconv.ParseBool(self.effectiveString(s))
}

func (self *Loader) effectiveUint16(v model.NumberVal) (uint16, error) {
	d, err := self.effectiveUint(v)
	if err != nil || d > 65536 {
		return 0, fmt.Errorf("invalid uint16: %q", self.effectiveString(string(v)))
	}
	return uint16(d), nil
}

func (self *Loader) effectiveUint(v model.NumberVal) (uint, error) {
	s := self.effectiveString(string(v))
	if s == "" {
		return 0, nil
	}
	d, err := strconv.Atoi(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid uint: %q", s)
	}
	return uint(d), nil
}

func (self *Loader) effectiveDuration(v, defaultVal string) (time.Duration, error) {
	v = self.effectiveString(v)
	if v == "" {
		v = defaultVal
	}
	if v == "" {
		return 0, nil
	}
	d, e := time.ParseDuration(v)
	if e != nil {
		return 0, fmt.Errorf("invalid duration: %q", v)
	}
	return d, nil
}

func toId(v string) string {
	return strings.Trim(toIdRegexp.ReplaceAllLiteralString(strings.ToLower(v), "-"), "-")
}

func absPath(p, basePath string) string {
	if len(p) > 0 && p[0:1] == "/" {
		return path.Clean(p)
	} else {
		return path.Join(path.Dir(basePath), p)
	}
}

func relPath(p, basePath string) string {
	p = path.Clean(p)
	if len(p) == 0 || p[0:1] == "/" {
		baseDir := path.Clean(path.Dir(basePath))
		switch {
		case p == baseDir:
			p = "."
		case strings.Index(p, baseDir+"/") == 0:
			p = p[len(baseDir)+1:]
		}
	}
	if isPath(p) {
		return p
	} else {
		return "./" + p
	}
}

func isPath(v string) bool {
	return "." == v || (len(v) > 0 && v[0:1] == "/") || (len(v) > 1 && v[0:2] == "./") || (len(v) > 2 && v[0:3] == "../")
}
