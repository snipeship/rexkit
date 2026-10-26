package main

import "rexkit/internal/install"

func runInstall(args []string) error   { return install.Main(args) }
func runUninstall(args []string) error { return install.UninstallMain(args) }
func runDoctor(args []string) error    { return install.DoctorMain(args) }
