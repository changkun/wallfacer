// GENERATED — DO NOT EDIT MANUALLY.
// Regenerate with: make api-contract
// Source: internal/apicontract/routes.go
//
// Usage:
//   fetch(Routes.env.get())                 // GET /api/env
//   fetch(task(id).diff())                  // GET /api/tasks/<id>/diff
//   new EventSource(Routes.tasks.stream())  // GET /api/tasks/stream

/* global Routes, task */

var Routes = {
  debug: {
    // GET /api/debug/health
    health: function () {
      return "/api/debug/health";
    },
    // GET /api/debug/spans
    spans: function () {
      return "/api/debug/spans";
    },
    // GET /api/debug/runtime
    runtime: function () {
      return "/api/debug/runtime";
    },
    // GET /api/debug/board
    board: function () {
      return "/api/debug/board";
    },
  },

  containers: {
    // GET /api/containers
    list: function () {
      return "/api/containers";
    },
  },

  images: {
    // GET /api/images
    status: function () {
      return "/api/images";
    },
    // POST /api/images/pull
    pull: function () {
      return "/api/images/pull";
    },
    // DELETE /api/images
    remove: function () {
      return "/api/images";
    },
    // GET /api/images/pull/stream
    pullStream: function () {
      return "/api/images/pull/stream";
    },
  },

  files: {
    // GET /api/files
    list: function () {
      return "/api/files";
    },
  },

  config: {
    // GET /api/config
    get: function () {
      return "/api/config";
    },
    // PUT /api/config
    update: function () {
      return "/api/config";
    },
  },

  workspaces: {
    // GET /api/workspaces/browse
    browse: function () {
      return "/api/workspaces/browse";
    },
    // POST /api/workspaces/mkdir
    mkdir: function () {
      return "/api/workspaces/mkdir";
    },
    // POST /api/workspaces/rename
    rename: function () {
      return "/api/workspaces/rename";
    },
    // PUT /api/workspaces
    update: function () {
      return "/api/workspaces";
    },
  },

  ideate: {
    // GET /api/ideate
    status: function () {
      return "/api/ideate";
    },
    // POST /api/ideate
    trigger: function () {
      return "/api/ideate";
    },
    // DELETE /api/ideate
    cancel: function () {
      return "/api/ideate";
    },
  },

  specs: {
    // GET /api/specs/tree
    tree: function () {
      return "/api/specs/tree";
    },
    // GET /api/specs/stream
    stream: function () {
      return "/api/specs/stream";
    },
  },

  planning: {
    // GET /api/planning
    status: function () {
      return "/api/planning";
    },
    // POST /api/planning
    start: function () {
      return "/api/planning";
    },
    // DELETE /api/planning
    stop: function () {
      return "/api/planning";
    },
    // GET /api/planning/messages
    messages: function () {
      return "/api/planning/messages";
    },
    // POST /api/planning/messages
    sendMessage: function () {
      return "/api/planning/messages";
    },
    // DELETE /api/planning/messages
    clearMessages: function () {
      return "/api/planning/messages";
    },
  },

  env: {
    // GET /api/env
    get: function () {
      return "/api/env";
    },
    // PUT /api/env
    update: function () {
      return "/api/env";
    },
    // POST /api/env/test
    test: function () {
      return "/api/env/test";
    },
  },

  instructions: {
    // GET /api/instructions
    get: function () {
      return "/api/instructions";
    },
    // PUT /api/instructions
    update: function () {
      return "/api/instructions";
    },
    // POST /api/instructions/reinit
    reinit: function () {
      return "/api/instructions/reinit";
    },
  },

  "system-prompts": {
    // GET /api/system-prompts
    list: function () {
      return "/api/system-prompts";
    },
    // GET /api/system-prompts/{name}
    get: function () {
      return "/api/system-prompts/{name}";
    },
    // PUT /api/system-prompts/{name}
    update: function () {
      return "/api/system-prompts/{name}";
    },
    // DELETE /api/system-prompts/{name}
    delete: function () {
      return "/api/system-prompts/{name}";
    },
  },

  templates: {
    // GET /api/templates
    list: function () {
      return "/api/templates";
    },
    // POST /api/templates
    create: function () {
      return "/api/templates";
    },
    // DELETE /api/templates/{id}
    delete: function () {
      return "/api/templates/{id}";
    },
  },

  git: {
    // GET /api/git/status
    status: function () {
      return "/api/git/status";
    },
    // GET /api/git/stream
    stream: function () {
      return "/api/git/stream";
    },
    // POST /api/git/push
    push: function () {
      return "/api/git/push";
    },
    // POST /api/git/sync
    sync: function () {
      return "/api/git/sync";
    },
    // POST /api/git/rebase-on-main
    rebaseOnMain: function () {
      return "/api/git/rebase-on-main";
    },
    // GET /api/git/branches
    branches: function () {
      return "/api/git/branches";
    },
    // POST /api/git/checkout
    checkout: function () {
      return "/api/git/checkout";
    },
    // POST /api/git/create-branch
    createBranch: function () {
      return "/api/git/create-branch";
    },
    // POST /api/git/open-folder
    openFolder: function () {
      return "/api/git/open-folder";
    },
  },

  usage: {
    // GET /api/usage
    stats: function () {
      return "/api/usage";
    },
  },

  stats: {
    // GET /api/stats
    get: function () {
      return "/api/stats";
    },
  },

  admin: {
    // POST /api/admin/rebuild-index
    rebuildIndex: function () {
      return "/api/admin/rebuild-index";
    },
  },

  explorer: {
    // GET /api/explorer/tree
    tree: function () {
      return "/api/explorer/tree";
    },
    // GET /api/explorer/stream
    stream: function () {
      return "/api/explorer/stream";
    },
    // GET /api/explorer/file
    readFile: function () {
      return "/api/explorer/file";
    },
    // PUT /api/explorer/file
    writeFile: function () {
      return "/api/explorer/file";
    },
  },

  auth: {
    // POST /api/auth/{provider}/start
    start: function () {
      return "/api/auth/{provider}/start";
    },
    // GET /api/auth/{provider}/status
    status: function () {
      return "/api/auth/{provider}/status";
    },
    // POST /api/auth/{provider}/cancel
    cancel: function () {
      return "/api/auth/{provider}/cancel";
    },
  },

  tasks: {
    // GET /api/tasks
    list: function () {
      return "/api/tasks";
    },
    // GET /api/tasks/stream
    stream: function () {
      return "/api/tasks/stream";
    },
    // POST /api/tasks
    create: function () {
      return "/api/tasks";
    },
    // POST /api/tasks/batch
    batchCreate: function () {
      return "/api/tasks/batch";
    },
    // POST /api/tasks/generate-titles
    generateTitles: function () {
      return "/api/tasks/generate-titles";
    },
    // POST /api/tasks/generate-oversight
    generateOversight: function () {
      return "/api/tasks/generate-oversight";
    },
    // GET /api/tasks/search
    search: function () {
      return "/api/tasks/search";
    },
    // POST /api/tasks/archive-done
    archiveDone: function () {
      return "/api/tasks/archive-done";
    },
    // GET /api/tasks/summaries
    summaries: function () {
      return "/api/tasks/summaries";
    },
    // GET /api/tasks/deleted
    listDeleted: function () {
      return "/api/tasks/deleted";
    },

    // task(id) returns an object with path-builder methods for
    // all task-instance endpoints. Use the top-level task() alias.
    task: function (id) {
      return {
        // GET /api/tasks/{id}/board
        board: function () {
          return "/api/tasks/" + id + "/board";
        },
        // PATCH /api/tasks/{id}
        update: function () {
          return "/api/tasks/" + id;
        },
        // DELETE /api/tasks/{id}
        delete: function () {
          return "/api/tasks/" + id;
        },
        // GET /api/tasks/{id}/events
        events: function () {
          return "/api/tasks/" + id + "/events";
        },
        // POST /api/tasks/{id}/feedback
        feedback: function () {
          return "/api/tasks/" + id + "/feedback";
        },
        // POST /api/tasks/{id}/done
        done: function () {
          return "/api/tasks/" + id + "/done";
        },
        // POST /api/tasks/{id}/cancel
        cancel: function () {
          return "/api/tasks/" + id + "/cancel";
        },
        // POST /api/tasks/{id}/resume
        resume: function () {
          return "/api/tasks/" + id + "/resume";
        },
        // POST /api/tasks/{id}/restore
        restore: function () {
          return "/api/tasks/" + id + "/restore";
        },
        // POST /api/tasks/{id}/archive
        archive: function () {
          return "/api/tasks/" + id + "/archive";
        },
        // POST /api/tasks/{id}/unarchive
        unarchive: function () {
          return "/api/tasks/" + id + "/unarchive";
        },
        // POST /api/tasks/{id}/sync
        sync: function () {
          return "/api/tasks/" + id + "/sync";
        },
        // POST /api/tasks/{id}/test
        test: function () {
          return "/api/tasks/" + id + "/test";
        },
        // GET /api/tasks/{id}/diff
        diff: function () {
          return "/api/tasks/" + id + "/diff";
        },
        // GET /api/tasks/{id}/logs
        logs: function () {
          return "/api/tasks/" + id + "/logs";
        },
        // GET /api/tasks/{id}/outputs/{filename}
        outputs: function (filename) {
          return "/api/tasks/" + id + "/outputs/" + filename;
        },
        // GET /api/tasks/{id}/turn-usage
        turnUsage: function () {
          return "/api/tasks/" + id + "/turn-usage";
        },
        // GET /api/tasks/{id}/spans
        spans: function () {
          return "/api/tasks/" + id + "/spans";
        },
        // GET /api/tasks/{id}/oversight
        oversight: function () {
          return "/api/tasks/" + id + "/oversight";
        },
        // GET /api/tasks/{id}/oversight/test
        oversightTest: function () {
          return "/api/tasks/" + id + "/oversight/test";
        },
        // POST /api/tasks/{id}/refine
        refine: function () {
          return "/api/tasks/" + id + "/refine";
        },
        // GET /api/tasks/{id}/refine/logs
        refineLogs: function () {
          return "/api/tasks/" + id + "/refine/logs";
        },
        // POST /api/tasks/{id}/refine/apply
        refineApply: function () {
          return "/api/tasks/" + id + "/refine/apply";
        },
        // POST /api/tasks/{id}/refine/dismiss
        refineDismiss: function () {
          return "/api/tasks/" + id + "/refine/dismiss";
        },
      };
    },
  },
};

// Convenience alias: task(id).diff(), task(id).logs(), etc.
var task = Routes.tasks.task;
