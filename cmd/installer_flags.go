package cmd

import "github.com/flanksource/deps/pkg/installer"

func newCLIInstaller() *installer.Installer {
	cacheDirToUse := cacheDir
	if cacheDirToUse == "" {
		cacheDirToUse = GetDepsConfig().Settings.CacheDir
	}

	return installer.NewWithConfig(
		GetDepsConfig(),
		installer.WithBinDir(binDir),
		installer.WithAppDir(appDir),
		installer.WithTmpDir(tmpDir),
		installer.WithCacheDir(cacheDirToUse),
		installer.WithForce(force),
		installer.WithSkipChecksum(skipChecksum),
		installer.WithStrictChecksum(strictChecksum),
		installer.WithDebug(debug),
		installer.WithOS(osOverride, archOverride),
		installer.WithTimeout(timeout),
		installer.WithIterateVersions(iterateVersions),
	)
}
