# Plugin System

I want to do a feasibility study on converting gearbox-agent and gearbox into a plugin first architecture.

Take a look at my very brief list of thoughts below, then use this document to create a study and step by step action list of how to accomplish the task.

The general idea is that we would rewrite both gearbox-agent and gearbox to be a framework consisting of shared components and the main interface and a set of plugins consisting of each of the items currently in the integrations list and the Dashboard.

The purpose of going to a plugin architecture is:

1. To have a clear separation of plugins from each other.  The ability to disable a plugin and know none of its code is running.  Loose coupling, etc.
2. Make it easier to write new plugins later
3. Have a clear set of documentation on how to write new plugins.  These docs should be human readable and AI Agent readable to ease the creation of future plugins.
4. To tailor the instance of the application to the needs of the consumer.

**Notes:**
From now on, integrations (logs, services, certificates, etc.) will be referred to as plugins
gearbox-agent and gearbox will be referred to as "The application"

## Framework

The framework would consist of:

- The left hand nav bar, top bar and content area
- Shared components like the toasts, dialogs, grids, icons, buttons, etc.
- Plugins manager (what is currently integrations) where plugins can be turned on or off per instance and configured.
- User manager, profile, change password, email settings, any other security related core items.
- Database backups
- Permissions
- I want you to deeply examine all of the code and plugins and determine everything that we could pull out of the plugins that could be shared and reused for the current set of plugins and any future we may want to write.

## Plugins

- Plugins should be self contained except for what they use from the Framework.  e.g. one plugin must not rely on code from another plugin, just from the Framework
- When a plugin is enabled it is injected into the sidebar and its code is activated throughout the application.  When it is disabled it is completely disabled and non of hte plugin code is running.
- Plugins appear in the plugin manager and can be configured there.  The configuration screen is part of the plugin, they simply inject into the plugin manager which will have the same basic capabilities as the current integrations system.
- Plugins tie into the permissions model so they can be assigned per user.  Can the user see the plugin, manage it, etc.
- Plugins will have a config page where the name the plugin shows in the nav bar can be set (it will have a default), what permission types it exposes, descriptive text, etc. Everything that the plugin manager and other tie in points will need.

## Other

Investigate how we would build and ship the plugins.  I like the single Go binary as it is small, compact and easy to ship, however, having the plugins be separate components is also appealing.  Can go dynamically load a compiled "go component" into itself without that component having the rest of the go system / GC, etc?
