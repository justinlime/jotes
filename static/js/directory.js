(function () {
  "use strict";

  var directoryView = document.querySelector("[data-directory-view='true']");
  var directoryTable = document.getElementById("directory-table");
  var directoryHeaderActions = document.getElementById("dir-header-actions");
  var backButton = document.getElementById("dir-back-btn");
  var createButton = document.getElementById("dir-create-btn");
  var editButton = document.getElementById("dir-edit-btn");
  var renameButton = document.getElementById("dir-rename-btn");
  var moveButton = document.getElementById("dir-move-btn");
  var deleteButton = document.getElementById("dir-delete-btn");
  var cancelButton = document.getElementById("dir-cancel-btn");
  var settingsControl = document.getElementById("dir-settings");
  var selectionBanner = document.getElementById("dir-selection-banner");
  var selectionCountLabel = document.getElementById("dir-selection-count");
  var selectAllCheckbox = document.getElementById("dir-select-all");
  var auxiliaryVisibilityCheckbox = document.getElementById("dir-setting-view-auxiliary");
  var hiddenVisibilityCheckbox = document.getElementById("dir-setting-show-hidden");
  var jotesVisibilityCheckbox = document.getElementById("dir-setting-show-jotes");

  if (!directoryView) {
    return;
  }

  var directoryState = {
    editMode: false,
  };

  /**
   * Return every directory-list row that represents one moveable or deletable
   * entry in the current listing.
   *
   * Parameters:
   *   - none: the function reads the cached directory table from the current page.
   *
   * Returns:
   *   - Array<HTMLElement>: each table row carrying entry metadata for directory actions.
   */
  function getDirectoryEntryRows() {
    if (!directoryTable || !directoryTable.tBodies || directoryTable.tBodies.length === 0) {
      return [];
    }

    return Array.prototype.slice.call(directoryTable.tBodies[0].querySelectorAll("tr[data-entry-path]"));
  }

  /**
   * Return the selection checkbox embedded in one directory-list row.
   *
   * Parameters:
   *   - row: the table row whose checkbox should be located.
   *
   * Returns:
   *   - HTMLInputElement|null: the row checkbox when present, otherwise null.
   */
  function getDirectoryRowCheckbox(row) {
    return row ? row.querySelector(".dir-select-checkbox") : null;
  }

  /**
   * Decide whether one directory row is currently visible after the existing
   * auxiliary and hidden-file filters have been applied.
   *
   * Parameters:
   *   - row: the directory row whose visibility should be inspected.
   *
   * Returns:
   *   - boolean: true when the row is currently visible to the user, otherwise false.
   */
  function isDirectoryRowVisible(row) {
    return Boolean(row) && !row.hidden;
  }

  /**
   * Build one normalized description of a directory-list row so move, delete,
   * and confirmation flows can work with plain data instead of DOM nodes.
   *
   * Parameters:
   *   - row: the directory row whose data attributes should be converted.
   *
   * Returns:
   *   - object|null: {path, name, kind, isJotesCompanion, row} when row is valid, otherwise null.
   */
  function readDirectoryRowEntry(row) {
    if (!row) {
      return null;
    }

    return {
      path: row.getAttribute("data-entry-path") || "",
      name: row.getAttribute("data-entry-name") || "",
      kind: row.getAttribute("data-entry-kind") || "file",
      isJotesCompanion: row.getAttribute("data-entry-is-jotes-companion") === "true",
      row: row,
    };
  }

  /**
   * Collect the currently selected directory entries in DOM order.
   *
   * Parameters:
   *   - none: the function scans the current directory table rows and their checkboxes.
   *
   * Returns:
   *   - Array<object>: selected entry descriptions shaped like readDirectoryRowEntry returns.
   */
  function getSelectedDirectoryEntries() {
    return getDirectoryEntryRows().filter(function (row) {
      var checkbox = getDirectoryRowCheckbox(row);
      return Boolean(checkbox && checkbox.checked);
    }).map(readDirectoryRowEntry).filter(Boolean);
  }

  /**
   * Collect only the selected entry paths for API payloads.
   *
   * Parameters:
   *   - none: the function delegates to getSelectedDirectoryEntries for the live selection.
   *
   * Returns:
   *   - Array<string>: selected entry paths in the order shown by the current directory listing.
   */
  function getSelectedDirectoryPaths() {
    return getSelectedDirectoryEntries().map(function (entry) {
      return entry.path;
    });
  }

  /**
   * Report whether the current selection includes any managed .jotes companion
   * folders or descendants that should move only with their owning note.
   *
   * Parameters:
   *   - selectedEntries: the normalized directory entries currently selected for an action.
   *
   * Returns:
   *   - boolean: true when selectedEntries contains at least one managed .jotes entry, otherwise false.
   */
  function selectionContainsJotesCompanionEntries(selectedEntries) {
    return Array.isArray(selectedEntries) && selectedEntries.some(function (entry) {
      return Boolean(entry && entry.isJotesCompanion);
    });
  }

  /**
   * Update row highlighting so edit mode clearly shows which entries are part
   * of the current bulk-action selection.
   *
   * Parameters:
   *   - none: the function reads every row checkbox from the current directory table.
   *
   * Returns:
   *   - none: each row receives or loses the is-selected class in place.
   */
  function syncDirectoryRowSelectionStyling() {
    getDirectoryEntryRows().forEach(function (row) {
      var checkbox = getDirectoryRowCheckbox(row);
      row.classList.toggle("is-selected", Boolean(checkbox && checkbox.checked));
    });
  }

  /**
   * Uncheck any selected rows that are no longer visible after directory filter
   * changes, preventing hidden selections from surprising the user.
   *
   * Parameters:
   *   - none: the function inspects the current table rows and their hidden states.
   *
   * Returns:
   *   - none: hidden selected rows are unchecked directly in the DOM.
   */
  function clearSelectionsForHiddenRows() {
    getDirectoryEntryRows().forEach(function (row) {
      var checkbox = getDirectoryRowCheckbox(row);
      if (!checkbox || !checkbox.checked || isDirectoryRowVisible(row)) {
        return;
      }
      checkbox.checked = false;
    });
  }

  /**
   * Clear every current row selection and reset the select-all checkbox state.
   *
   * Parameters:
   *   - none: the function updates every selection control in the current directory table.
   *
   * Returns:
   *   - none: all row checkboxes are unchecked and the header checkbox is reset.
   */
  function clearDirectorySelection() {
    getDirectoryEntryRows().forEach(function (row) {
      var checkbox = getDirectoryRowCheckbox(row);
      if (checkbox) {
        checkbox.checked = false;
      }
    });

    if (selectAllCheckbox) {
      selectAllCheckbox.checked = false;
      selectAllCheckbox.indeterminate = false;
    }

    syncDirectoryRowSelectionStyling();
  }

  /**
   * Recount the visible directory header actions so CSS can choose the correct
   * mobile grid arrangement for two-, three-, and four-button states.
   *
   * Parameters:
   *   - none: the function inspects the cached header action controls on the current page.
   *
   * Returns:
   *   - none: the header action container's data-visible-actions attribute is updated in place.
   */
  function syncDirectoryHeaderActionLayout() {
    var visibleCount = 0;
    var headerActionElements = [backButton, createButton, editButton, renameButton, moveButton, deleteButton, cancelButton, settingsControl];

    if (!directoryHeaderActions) {
      return;
    }

    headerActionElements.forEach(function (element) {
      if (!element || element.hidden) {
        return;
      }
      visibleCount += 1;
    });

    directoryHeaderActions.setAttribute("data-visible-actions", String(visibleCount));
  }

  /**
   * Update the header buttons, selection badge, and select-all checkbox so the
   * current edit-mode state stays in sync with the selected rows.
   *
   * Parameters:
   *   - none: the function reads the live selection and toggles existing cached controls.
   *
   * Returns:
   *   - none: directory action controls are enabled, disabled, shown, or hidden in place.
   */
  function syncDirectoryActionControls() {
    var selectedEntries = getSelectedDirectoryEntries();
    var selectedCount = selectedEntries.length;
    var visibleRows = getDirectoryEntryRows().filter(isDirectoryRowVisible);
    var visibleSelectedCount = visibleRows.filter(function (row) {
      var checkbox = getDirectoryRowCheckbox(row);
      return Boolean(checkbox && checkbox.checked);
    }).length;
    var hasEntries = directoryView.getAttribute("data-has-entries") === "true";

    if (backButton) {
      backButton.hidden = directoryState.editMode;
    }
    if (createButton) {
      createButton.hidden = directoryState.editMode;
    }
    if (editButton) {
      editButton.hidden = directoryState.editMode;
      editButton.disabled = !hasEntries;
    }
    if (renameButton) {
      renameButton.hidden = !directoryState.editMode;
      renameButton.disabled = selectedCount !== 1;
    }
    if (moveButton) {
      moveButton.hidden = !directoryState.editMode;
      moveButton.disabled = selectedCount === 0;
    }
    if (deleteButton) {
      deleteButton.hidden = !directoryState.editMode;
      deleteButton.disabled = selectedCount === 0;
    }
    if (cancelButton) {
      cancelButton.hidden = !directoryState.editMode;
    }
    if (settingsControl) {
      settingsControl.hidden = directoryState.editMode;
    }
    if (selectionBanner) {
      selectionBanner.hidden = !directoryState.editMode;
    }
    if (selectionCountLabel) {
      selectionCountLabel.textContent = selectedCount === 1 ? "1 selected" : String(selectedCount) + " selected";
    }

    if (selectAllCheckbox) {
      selectAllCheckbox.hidden = !directoryState.editMode;
      selectAllCheckbox.checked = visibleRows.length > 0 && visibleSelectedCount === visibleRows.length;
      selectAllCheckbox.indeterminate = visibleSelectedCount > 0 && visibleSelectedCount < visibleRows.length;
    }

    syncDirectoryHeaderActionLayout();
    syncDirectoryRowSelectionStyling();
  }

  /**
   * Enter or leave directory edit mode, optionally clearing any existing row
   * selection when the user exits.
   *
   * Parameters:
   *   - nextEditMode: true to enable bulk-selection mode, or false to cancel it.
   *
   * Returns:
   *   - none: the page-level editing class and related controls are updated immediately.
   */
  function setDirectoryEditMode(nextEditMode) {
    directoryState.editMode = Boolean(nextEditMode) && Boolean(directoryTable);
    directoryView.classList.toggle("is-editing", directoryState.editMode);

    if (!directoryState.editMode) {
      clearDirectorySelection();
    }

    syncDirectoryActionControls();
  }

  /**
   * Show one themed toast message using the shared site-wide notification API,
   * falling back to a browser alert only when the shared helper is unavailable.
   *
   * Parameters:
   *   - kind: toast variant string, typically "success" or "error".
   *   - message: user-facing text that should explain the outcome.
   *
   * Returns:
   *   - none: the message is displayed immediately through the available UI channel.
   */
  function showDirectoryToast(kind, message) {
    if (window.jotesShowToastNotification) {
      window.jotesShowToastNotification({ kind: kind, message: message });
      return;
    }

    window.alert(message);
  }

  /**
   * Return the shared toast region used by upload-progress notifications,
   * reusing the site-wide region when it already exists and creating it only
   * as a fallback when the page has not shown any other toasts yet.
   *
   * Parameters:
   *   - none: the function inspects the live document for the shared toast region.
   *
   * Returns:
   *   - HTMLElement|null: the toast region container, or null when the document body is unavailable.
   */
  function getDirectoryToastRegion() {
    var existingRegion = document.querySelector(".jotes-toast-region");
    var region = null;

    if (existingRegion) {
      return existingRegion;
    }

    if (!document.body) {
      return null;
    }

    region = document.createElement("div");
    region.className = "jotes-toast-region";
    region.setAttribute("aria-live", "polite");
    region.setAttribute("aria-atomic", "true");
    document.body.appendChild(region);
    return region;
  }

  /**
   * Create one persistent upload-progress toast that can be updated in place as
   * the browser streams file data to the server.
   *
   * Parameters:
   *   - fileName: the basename of the file currently being uploaded.
   *
   * Returns:
   *   - object|null: {element, messageElement, progressElement, fillElement, dismissTimer, removalTimer} when the toast UI could be created, otherwise null.
   */
  function createDirectoryUploadToast(fileName) {
    var region = getDirectoryToastRegion();
    var toast = null;
    var message = null;
    var progress = null;
    var fill = null;
    var toastController = null;

    if (!region) {
      return null;
    }

    toast = document.createElement("div");
    toast.className = "jotes-toast jotes-toast--progress";
    toast.setAttribute("role", "status");

    message = document.createElement("div");
    message.className = "jotes-toast-copy";
    message.textContent = "Preparing upload for " + fileName + "...";

    progress = document.createElement("div");
    progress.className = "jotes-toast-progress";
    fill = document.createElement("div");
    fill.className = "jotes-toast-progress-fill is-indeterminate";
    progress.appendChild(fill);
    toast.appendChild(message);
    toast.appendChild(progress);
    region.appendChild(toast);

    toastController = {
      element: toast,
      messageElement: message,
      progressElement: progress,
      fillElement: fill,
      dismissTimer: 0,
      removalTimer: 0,
    };

    toast.addEventListener("click", function () {
      dismissDirectoryUploadToast(toastController, 0);
    });

    window.requestAnimationFrame(function () {
      toast.classList.add("is-visible");
    });

    return toastController;
  }

  /**
   * Update one persistent upload-progress toast with the newest message,
   * variant, and percent-complete state.
   *
   * Parameters:
   *   - toastController: the controller object returned by createDirectoryUploadToast.
   *   - options: object shaped like {kind, message, progress, indeterminate, hideProgress}.
   *
   * Returns:
   *   - none: the toast DOM is updated immediately when toastController is valid.
   */
  function updateDirectoryUploadToast(toastController, options) {
    var kind = options && typeof options.kind === "string" ? options.kind : "progress";
    var message = options && typeof options.message === "string" ? options.message : "";
    var progressValue = options && typeof options.progress === "number" ? options.progress : null;
    var isIndeterminate = !progressValue && !(options && options.indeterminate === false);

    if (!toastController || !toastController.element) {
      return;
    }

    toastController.element.setAttribute("role", kind === "error" ? "alert" : "status");
    toastController.element.classList.remove("jotes-toast--progress", "jotes-toast--success", "jotes-toast--error");
    if (kind === "error") {
      toastController.element.classList.add("jotes-toast--error");
    } else if (kind === "success") {
      toastController.element.classList.add("jotes-toast--success");
    } else {
      toastController.element.classList.add("jotes-toast--progress");
    }

    if (message) {
      toastController.messageElement.textContent = message;
    }

    toastController.progressElement.hidden = Boolean(options && options.hideProgress);
    if (toastController.progressElement.hidden) {
      return;
    }

    if (progressValue !== null) {
      progressValue = Math.max(0, Math.min(100, Math.round(progressValue)));
      isIndeterminate = false;
      toastController.fillElement.style.width = String(progressValue) + "%";
    } else {
      toastController.fillElement.style.width = "100%";
    }

    toastController.fillElement.classList.toggle("is-indeterminate", Boolean(isIndeterminate));
  }

  /**
   * Hide and remove one persistent upload-progress toast, either immediately or
   * after a short caller-supplied delay.
   *
   * Parameters:
   *   - toastController: the controller object returned by createDirectoryUploadToast.
   *   - delayMs: how long to wait before starting the dismiss animation; zero removes it immediately.
   *
   * Returns:
   *   - none: the toast is detached from the DOM after its exit animation completes.
   */
  function dismissDirectoryUploadToast(toastController, delayMs) {
    var startDismiss = null;

    if (!toastController || !toastController.element) {
      return;
    }

    window.clearTimeout(toastController.dismissTimer);
    window.clearTimeout(toastController.removalTimer);
    toastController.dismissTimer = 0;
    toastController.removalTimer = 0;

    startDismiss = function () {
      var toastElement = toastController.element;

      toastController.element = null;
      toastController.messageElement = null;
      toastController.progressElement = null;
      toastController.fillElement = null;
      if (!toastElement) {
        return;
      }

      toastElement.classList.remove("is-visible");
      toastController.removalTimer = window.setTimeout(function () {
        if (toastElement.parentNode) {
          toastElement.parentNode.removeChild(toastElement);
        }
      }, 200);
    };

    if (typeof delayMs === "number" && delayMs > 0) {
      toastController.dismissTimer = window.setTimeout(startDismiss, delayMs);
      return;
    }

    startDismiss();
  }

  /**
   * Prompt the user to choose one local file for upload using the same modal
   * shell as the other directory create flows.
   *
   * Parameters:
   *   - none: the dialog reads the current directory path from the page data attributes.
   *
   * Returns:
   *   - Promise<File|null>: the chosen browser File object when confirmed, otherwise null.
   */
  function promptForUploadFile() {
    var currentPath = directoryView.getAttribute("data-current-path") || "/";
    var dialogController = createDirectoryDialog({
      title: "Upload file",
      description: "Choose one file from your computer to upload into the current folder.",
    });
    var field = document.createElement("div");
    var label = document.createElement("label");
    var input = document.createElement("input");
    var hint = document.createElement("p");
    var errorMessage = document.createElement("p");
    var confirmAction = document.createElement("button");
    var cancelAction = document.createElement("button");
    var inputId = "jotes-upload-input-" + String(Date.now()) + "-" + String(Math.floor(Math.random() * 1000));

    /**
     * Synchronize the upload dialog controls with the current file-picker
     * selection so the user can only confirm once a file is chosen.
     *
     * Parameters:
     *   - none: the function reads the live file input selection.
     *
     * Returns:
     *   - none: button and helper text state update in place.
     */
    function syncUploadSelectionState() {
      var selectedFile = input.files && input.files.length ? input.files[0] : null;

      confirmAction.disabled = !selectedFile;
      hint.textContent = "Upload into: " + currentPath + (selectedFile ? " — Selected file: " + selectedFile.name : "");
      errorMessage.hidden = true;
      errorMessage.textContent = "";
    }

    /**
     * Validate the file-picker state and resolve the dialog with the selected
     * File object when the user confirms the upload.
     *
     * Parameters:
     *   - none: the function reads the current input selection from the dialog.
     *
     * Returns:
     *   - none: the dialog closes with the selected File when validation succeeds.
     */
    function submitUploadSelection() {
      var selectedFile = input.files && input.files.length ? input.files[0] : null;

      if (!selectedFile) {
        errorMessage.hidden = false;
        errorMessage.textContent = "Choose a file before starting the upload.";
        input.focus();
        return;
      }

      dialogController.close(selectedFile);
    }

    field.className = "jotes-directory-field";
    label.className = "jotes-directory-label";
    label.setAttribute("for", inputId);
    label.textContent = "File";
    input.className = "jotes-directory-file-input";
    input.id = inputId;
    input.type = "file";
    hint.className = "jotes-directory-result-relative";
    hint.textContent = "Upload into: " + currentPath;
    errorMessage.className = "jotes-directory-result-note";
    errorMessage.hidden = true;

    confirmAction.className = "btn btn-success";
    confirmAction.type = "button";
    confirmAction.textContent = "Upload file";
    confirmAction.disabled = true;
    cancelAction.className = "btn btn-danger";
    cancelAction.type = "button";
    cancelAction.textContent = "Cancel";

    field.appendChild(label);
    field.appendChild(input);
    field.appendChild(hint);
    field.appendChild(errorMessage);
    dialogController.body.appendChild(field);
    dialogController.actions.appendChild(confirmAction);
    dialogController.actions.appendChild(cancelAction);

    cancelAction.addEventListener("click", function () {
      dialogController.close(null);
    });
    confirmAction.addEventListener("click", submitUploadSelection);
    input.addEventListener("change", syncUploadSelectionState);

    window.setTimeout(function () {
      input.focus();
    }, 0);

    return dialogController.promise;
  }

  /**
   * Decode one XMLHttpRequest upload response into a JSON payload when possible
   * so upload success and error handling can reuse the server's structured
   * messages.
   *
   * Parameters:
   *   - request: the completed XMLHttpRequest whose responseText may contain JSON.
   *
   * Returns:
   *   - object|null: the parsed JSON payload, or null when the response is empty or invalid.
   */
  function decodeDirectoryUploadResponse(request) {
    if (!request || typeof request.responseText !== "string" || !request.responseText) {
      return null;
    }

    try {
      return JSON.parse(request.responseText);
    } catch (_error) {
      return null;
    }
  }

  /**
   * Upload one browser-selected file into the current directory while showing a
   * toast-style progress indicator that updates during the transfer.
   *
   * Parameters:
   *   - file: the browser File object that should be uploaded into the current directory.
   *
   * Returns:
   *   - Promise<void>: resolves after the upload succeeds and a follow-up reload has been scheduled.
   */
  function uploadDirectoryFile(file) {
    var currentPath = directoryView.getAttribute("data-current-path") || "/";
    var uploadToast = createDirectoryUploadToast(file && file.name ? file.name : "file");

    return new Promise(function (resolve, reject) {
      var request = new window.XMLHttpRequest();
      var formData = new window.FormData();

      formData.append("parent", currentPath);
      formData.append("file", file, file.name);

      updateDirectoryUploadToast(uploadToast, {
        kind: "progress",
        message: "Uploading " + file.name + "...",
        indeterminate: true,
      });

      request.open("POST", "/jotes/api/files/upload", true);
      request.setRequestHeader("Accept", "application/json");

      request.upload.addEventListener("progress", function (event) {
        if (!event.lengthComputable || event.total <= 0) {
          updateDirectoryUploadToast(uploadToast, {
            kind: "progress",
            message: "Uploading " + file.name + "...",
            indeterminate: true,
          });
          return;
        }

        updateDirectoryUploadToast(uploadToast, {
          kind: "progress",
          message: "Uploading " + file.name + "... " + String(Math.max(0, Math.min(100, Math.round((event.loaded / event.total) * 100)))) + "%",
          progress: (event.loaded / event.total) * 100,
          indeterminate: false,
        });
      });

      request.addEventListener("load", function () {
        var payload = decodeDirectoryUploadResponse(request);
        var error = null;
        var hasToastFeedback = Boolean(uploadToast && uploadToast.element);

        if (request.status >= 200 && request.status < 300 && payload && payload.success !== false) {
          if (hasToastFeedback) {
            updateDirectoryUploadToast(uploadToast, {
              kind: "success",
              message: "Uploaded " + file.name + ". Refreshing...",
              progress: 100,
              indeterminate: false,
            });
          } else {
            showDirectoryToast("success", "Uploaded " + file.name + ". Refreshing...");
          }
          reloadDirectoryPage(900);
          resolve();
          return;
        }

        error = new Error(payload && payload.error ? payload.error : "The selected file could not be uploaded.");
        error.directoryFeedbackShown = hasToastFeedback;
        updateDirectoryUploadToast(uploadToast, {
          kind: "error",
          message: error.message,
          hideProgress: true,
        });
        dismissDirectoryUploadToast(uploadToast, 5600);
        reject(error);
      });

      request.addEventListener("error", function () {
        var error = new Error("The selected file could not be uploaded.");
        var hasToastFeedback = Boolean(uploadToast && uploadToast.element);

        error.directoryFeedbackShown = hasToastFeedback;
        updateDirectoryUploadToast(uploadToast, {
          kind: "error",
          message: error.message,
          hideProgress: true,
        });
        dismissDirectoryUploadToast(uploadToast, 5600);
        reject(error);
      });

      request.addEventListener("abort", function () {
        var error = new Error("The upload was cancelled.");
        var hasToastFeedback = Boolean(uploadToast && uploadToast.element);

        error.directoryFeedbackShown = hasToastFeedback;
        updateDirectoryUploadToast(uploadToast, {
          kind: "error",
          message: error.message,
          hideProgress: true,
        });
        dismissDirectoryUploadToast(uploadToast, 4200);
        reject(error);
      });

      request.send(formData);
    });
  }

  /**
   * Reload the current page after a short optional delay so successful bulk
   * file operations immediately refresh the directory listing.
   *
   * Parameters:
   *   - delayMs: optional delay before reloading, in milliseconds.
   *
   * Returns:
   *   - none: the current browser location is reloaded after the requested delay.
   */
  function reloadDirectoryPage(delayMs) {
    window.setTimeout(function () {
      window.location.reload();
    }, typeof delayMs === "number" && delayMs > 0 ? delayMs : 0);
  }

  /**
   * Convert one count into a human-readable singular or plural label for user
   * messages and dialog copy.
   *
   * Parameters:
   *   - count: the number of selected entries being described.
   *   - singular: the word to use when count equals one.
   *   - plural: the word to use when count does not equal one.
   *
   * Returns:
   *   - string: a short count phrase such as "1 item" or "3 items".
   */
  function formatCountLabel(count, singular, plural) {
    return String(count) + " " + (count === 1 ? singular : plural);
  }

  /**
   * Build a short list of selected entry names for dialog copy, truncating the
   * output when many entries are selected.
   *
   * Parameters:
   *   - entries: the selected directory entry objects that should be summarized.
   *
   * Returns:
   *   - string: a comma-separated summary of up to three entry names plus an overflow note.
   */
  function summarizeSelectedEntryNames(entries) {
    var names = entries.map(function (entry) {
      return entry.name;
    });

    if (names.length <= 3) {
      return names.join(", ");
    }

    return names.slice(0, 3).join(", ") + ", and " + String(names.length - 3) + " more";
  }

  /**
   * Create one reusable themed dialog shell with focus trapping, Escape-to-close,
   * outside-click cancellation, and a Promise that resolves exactly once.
   *
   * Parameters:
   *   - options: object shaped like {title, description, wide} describing the dialog heading and layout.
   *
   * Returns:
   *   - object: {overlay, dialog, body, actions, close, promise} for callers to populate and resolve.
   */
  function createDirectoryDialog(options) {
    var overlay = document.createElement("div");
    var dialog = document.createElement("div");
    var header = document.createElement("div");
    var title = document.createElement("h2");
    var description = document.createElement("p");
    var body = document.createElement("div");
    var actions = document.createElement("div");
    var titleId = "jotes-directory-dialog-title-" + String(Date.now()) + "-" + String(Math.floor(Math.random() * 1000));
    var descriptionId = titleId + "-copy";
    var previouslyFocusedElement = document.activeElement;
    var resolved = false;
    var resolvePromise = null;

    overlay.className = "jotes-directory-overlay";
    dialog.className = "jotes-directory-dialog" + (options && options.wide ? " jotes-directory-dialog--wide" : "");
    dialog.setAttribute("role", "dialog");
    dialog.setAttribute("aria-modal", "true");
    dialog.setAttribute("aria-labelledby", titleId);
    dialog.setAttribute("aria-describedby", descriptionId);

    header.className = "jotes-directory-dialog-header";
    title.className = "jotes-directory-dialog-title";
    title.id = titleId;
    title.textContent = options && options.title ? options.title : "Directory action";
    description.className = "jotes-directory-dialog-copy";
    description.id = descriptionId;
    description.textContent = options && options.description ? options.description : "Review the details below before continuing.";
    body.className = "jotes-directory-dialog-form";
    actions.className = "jotes-directory-dialog-actions";

    header.appendChild(title);
    header.appendChild(description);
    dialog.appendChild(header);
    dialog.appendChild(body);
    dialog.appendChild(actions);
    overlay.appendChild(dialog);

    /**
     * Return every currently focusable element inside the live dialog so Tab
     * navigation can be trapped within the modal surface.
     *
     * Parameters:
     *   - none: the function scans the elements already inserted into the dialog.
     *
     * Returns:
     *   - Array<HTMLElement>: focusable dialog descendants in document order.
     */
    function getFocusableDialogElements() {
      return Array.prototype.slice.call(dialog.querySelectorAll("button, [href], input, select, textarea, [tabindex]:not([tabindex='-1'])")).filter(function (element) {
        return !element.disabled && element.offsetParent !== null;
      });
    }

    /**
     * Resolve and close the dialog exactly once, remove listeners, and restore
     * focus to the element that was active before the modal opened.
     *
     * Parameters:
     *   - result: the value that should resolve the caller-facing dialog promise.
     *
     * Returns:
     *   - none: the overlay is removed from the DOM and the promise resolves once.
     */
    function closeDirectoryDialog(result) {
      if (resolved) {
        return;
      }

      resolved = true;
      document.removeEventListener("keydown", handleDirectoryDialogKeydown);
      if (overlay.parentNode) {
        overlay.parentNode.removeChild(overlay);
      }
      if (previouslyFocusedElement && typeof previouslyFocusedElement.focus === "function") {
        previouslyFocusedElement.focus();
      }
      if (resolvePromise) {
        resolvePromise(result);
      }
    }

    /**
     * Trap Tab focus inside the open dialog and let Escape close it as a cancel
     * action without affecting the rest of the page.
     *
     * Parameters:
     *   - event: the document-level keydown event raised while the dialog is open.
     *
     * Returns:
     *   - none: focus is cycled or the dialog is cancelled when relevant keys are pressed.
     */
    function handleDirectoryDialogKeydown(event) {
      var focusableElements = null;
      var currentIndex = -1;

      if (event.key === "Escape") {
        event.preventDefault();
        closeDirectoryDialog(null);
        return;
      }

      if (event.key !== "Tab") {
        return;
      }

      focusableElements = getFocusableDialogElements();
      if (focusableElements.length === 0) {
        event.preventDefault();
        return;
      }

      currentIndex = focusableElements.indexOf(document.activeElement);
      event.preventDefault();

      if (event.shiftKey) {
        focusableElements[(currentIndex <= 0 ? focusableElements.length : currentIndex) - 1].focus();
        return;
      }

      focusableElements[(currentIndex + 1) % focusableElements.length].focus();
    }

    overlay.addEventListener("click", function (event) {
      if (event.target === overlay) {
        closeDirectoryDialog(null);
      }
    });

    document.addEventListener("keydown", handleDirectoryDialogKeydown);
    document.body.appendChild(overlay);

    return {
      overlay: overlay,
      dialog: dialog,
      body: body,
      actions: actions,
      close: closeDirectoryDialog,
      promise: new Promise(function (resolve) {
        resolvePromise = resolve;
      }),
    };
  }

  /**
   * Set one inline validation or status message in a dialog body element,
   * toggling visibility automatically when the message is empty.
   *
   * Parameters:
   *   - element: the message node whose text should be updated.
   *   - message: the user-facing text to show, or an empty string to hide it.
   *
   * Returns:
   *   - none: element text and hidden state are updated in place.
   */
  function setDirectoryDialogMessage(element, message) {
    var normalizedMessage = typeof message === "string" ? message.trim() : "";
    element.textContent = normalizedMessage;
    element.hidden = normalizedMessage === "";
  }

  /**
   * Show the initial Create chooser so the user can decide whether the next
   * flow should create a note, create a directory, or upload a local file.
   *
   * Parameters:
   *   - none: the dialog is self-contained and returns only the selected create mode.
   *
   * Returns:
   *   - Promise<string|null>: "note", "directory", or "upload" when selected, or null when cancelled.
   */
  function promptForCreateOption() {
    var dialogController = createDirectoryDialog({
      title: "Create",
      description: "Choose what you want to create or upload in the current folder.",
    });
    var createNoteAction = document.createElement("button");
    var createDirectoryAction = document.createElement("button");
    var uploadFileAction = document.createElement("button");
    var cancelAction = document.createElement("button");

    createNoteAction.className = "btn btn-success";
    createNoteAction.type = "button";
    createNoteAction.textContent = "Create note";
    createDirectoryAction.className = "btn btn-blue";
    createDirectoryAction.type = "button";
    createDirectoryAction.textContent = "Create directory";
    uploadFileAction.className = "btn btn-primary";
    uploadFileAction.type = "button";
    uploadFileAction.textContent = "Upload file";
    cancelAction.className = "btn btn-danger";
    cancelAction.type = "button";
    cancelAction.textContent = "Cancel";

    dialogController.actions.appendChild(createNoteAction);
    dialogController.actions.appendChild(createDirectoryAction);
    dialogController.actions.appendChild(uploadFileAction);
    dialogController.actions.appendChild(cancelAction);

    cancelAction.addEventListener("click", function () {
      dialogController.close(null);
    });
    createNoteAction.addEventListener("click", function () {
      dialogController.close("note");
    });
    createDirectoryAction.addEventListener("click", function () {
      dialogController.close("directory");
    });
    uploadFileAction.addEventListener("click", function () {
      dialogController.close("upload");
    });

    return dialogController.promise;
  }

  /**
   * Show one single-input dialog that collects a non-empty text value for a
   * specific create flow such as note names or directory names.
   *
   * Parameters:
   *   - options: object shaped like {title, description, label, placeholder, hint, submitLabel, emptyMessage, submitValue}.
   *
   * Returns:
   *   - Promise<object|null>: {value, submitValue} when the user confirms, or null when cancelled.
   */
  function promptForCreateInput(options) {
    var dialogController = createDirectoryDialog({
      title: options && options.title ? options.title : "Create",
      description: options && options.description ? options.description : "Enter the requested value below.",
    });
    var field = document.createElement("div");
    var label = document.createElement("label");
    var input = document.createElement("input");
    var hint = document.createElement("p");
    var errorMessage = document.createElement("p");
    var confirmAction = document.createElement("button");
    var cancelAction = document.createElement("button");
    var inputId = "jotes-create-input-" + String(Date.now()) + "-" + String(Math.floor(Math.random() * 1000));

    /**
     * Validate the current input value and resolve the dialog with the trimmed
     * user input when the value is present.
     *
     * Parameters:
     *   - none: the function reads the current field state from the surrounding dialog.
     *
     * Returns:
     *   - none: the dialog closes with the normalized input payload when validation succeeds.
     */
    function submitCreateInput() {
      var value = input.value.trim();

      if (!value) {
        setDirectoryDialogMessage(errorMessage, options && options.emptyMessage ? options.emptyMessage : "Enter a value before continuing.");
        input.focus();
        return;
      }

      dialogController.close({
        value: value,
        submitValue: options && options.submitValue ? options.submitValue : "",
      });
    }

    field.className = "jotes-directory-field";
    label.className = "jotes-directory-label";
    label.setAttribute("for", inputId);
    label.textContent = options && options.label ? options.label : "Value";
    input.className = "jotes-directory-input";
    input.id = inputId;
    input.type = options && options.type ? options.type : "text";
    input.placeholder = options && options.placeholder ? options.placeholder : "";
    input.autocomplete = "off";
    input.autocapitalize = "off";
    input.spellcheck = false;
    hint.className = "jotes-directory-result-relative";
    hint.textContent = options && options.hint ? options.hint : "";
    hint.hidden = !(options && options.hint);
    errorMessage.className = "jotes-directory-result-note";
    errorMessage.hidden = true;

    confirmAction.className = "btn btn-success";
    confirmAction.type = "button";
    confirmAction.textContent = options && options.submitLabel ? options.submitLabel : "Continue";
    cancelAction.className = "btn btn-danger";
    cancelAction.type = "button";
    cancelAction.textContent = "Cancel";

    field.appendChild(label);
    field.appendChild(input);
    field.appendChild(hint);
    field.appendChild(errorMessage);
    dialogController.body.appendChild(field);
    dialogController.actions.appendChild(confirmAction);
    dialogController.actions.appendChild(cancelAction);

    cancelAction.addEventListener("click", function () {
      dialogController.close(null);
    });
    confirmAction.addEventListener("click", submitCreateInput);
    input.addEventListener("keydown", function (event) {
      if (event.key === "Enter") {
        event.preventDefault();
        submitCreateInput();
      }
    });

    window.setTimeout(function () {
      input.focus();
      input.select();
    }, 0);

    return dialogController.promise;
  }

  /**
   * Prompt for one new note basename and shape the response like the existing
   * managed create API expects.
   *
   * Parameters:
   *   - none: the dialog is self-contained and only returns the final note create request.
   *
   * Returns:
   *   - Promise<object|null>: {name, type:"file"} when confirmed, or null when cancelled.
   */
  function promptForNewNoteEntry() {
    return promptForCreateInput({
      title: "Create note",
      description: "Enter the note name that should be created in the current folder.",
      label: "Name",
      placeholder: "meeting-notes or reference",
      hint: "Notes default to Markdown when you omit an extension.",
      submitLabel: "Create note",
      emptyMessage: "Enter a name before creating the note.",
      submitValue: "file",
    }).then(function (result) {
      if (!result) {
        return null;
      }

      return {
        name: result.value,
        type: "file",
      };
    });
  }

  /**
   * Prompt for one new directory basename and shape the response like the
   * existing managed create API expects.
   *
   * Parameters:
   *   - none: the dialog is self-contained and only returns the final directory create request.
   *
   * Returns:
   *   - Promise<object|null>: {name, type:"directory"} when confirmed, or null when cancelled.
   */
  function promptForNewDirectoryName() {
    return promptForCreateInput({
      title: "Create directory",
      description: "Enter the directory name that should be created in the current folder.",
      label: "Name",
      placeholder: "reference or projects",
      hint: "Directories are created exactly as named.",
      submitLabel: "Create directory",
      emptyMessage: "Enter a name before creating the directory.",
      submitValue: "directory",
    }).then(function (result) {
      if (!result) {
        return null;
      }

      return {
        name: result.value,
        type: "directory",
      };
    });
  }

  /**
   * Send one JSON request to the server and normalize success and error cases
   * into a single Promise flow for directory actions.
   *
   * Parameters:
   *   - url: the same-origin endpoint that should receive the request.
   *   - options: fetch options object including method, headers, body, and optional signal.
   *
   * Returns:
   *   - Promise<object>: the parsed JSON payload when the request succeeds.
   */
  function sendDirectoryJSONRequest(url, options) {
    return window.fetch(url, options).then(function (response) {
      return response.json().catch(function () {
        return { success: false, error: "The server returned an unreadable response." };
      }).then(function (payload) {
        if (!response.ok || !payload || payload.success === false) {
          throw new Error(payload && payload.error ? payload.error : "The request could not be completed.");
        }
        return payload;
      });
    });
  }

  /**
   * Create one new note or directory in the current directory and navigate to
   * the most useful follow-up page for the created entry.
   *
   * Parameters:
   *   - createRequest: object shaped like {name, type} where type is "file" or "directory".
   *
   * Returns:
   *   - Promise<void>: resolves after the follow-up navigation has been triggered.
   */
  function createDirectoryEntry(createRequest) {
    var currentPath = directoryView.getAttribute("data-current-path") || "/";
    var entryType = createRequest && createRequest.type === "directory" ? "directory" : "file";

    return sendDirectoryJSONRequest("/jotes/api/files/create", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Accept": "application/json",
      },
      body: JSON.stringify({
        name: createRequest ? createRequest.name : "",
        type: entryType,
        parent: currentPath,
        content: "",
      }),
    }).then(function (payload) {
      if (entryType === "directory") {
        window.location.href = payload.path;
        return;
      }

      window.location.href = "/jotes/edit" + payload.path;
    });
  }

  /**
   * Fetch candidate destination directories for the move dialog, scoped to the
   * current note root and optionally filtered by the user's query.
   *
   * Parameters:
   *   - query: the current search text typed into the move dialog.
   *   - currentPath: the directory path currently being viewed, used for relative-path hints.
   *   - signal: optional AbortSignal used to cancel stale in-flight requests.
   *
   * Returns:
   *   - Promise<object>: the parsed directory-list response from the backend.
   */
  function fetchMoveDestinationDirectories(query, currentPath, signal) {
    var searchParams = new window.URLSearchParams();
    searchParams.set("q", query || "");
    searchParams.set("from", currentPath || "/");

    return sendDirectoryJSONRequest("/jotes/api/directories?" + searchParams.toString(), {
      method: "GET",
      headers: {
        "Accept": "application/json",
      },
      signal: signal,
    });
  }

  /**
   * Decide whether one candidate destination directory is valid for the current
   * selected entries, producing a short explanation when the destination should
   * be disabled in the move dialog.
   *
   * Parameters:
   *   - selectedEntries: the current bulk-selection entries that may be moved.
   *   - destinationPath: the candidate destination directory path returned by the backend.
   *   - currentPath: the directory path currently being viewed.
   *
   * Returns:
   *   - object: {allowed, note} describing whether the destination can be chosen and why.
   */
  function evaluateMoveDestination(selectedEntries, destinationPath, currentPath) {
    var candidatePath = destinationPath || "/";
    var index = 0;

    if (candidatePath === currentPath) {
      return {
        allowed: false,
        note: "The selected items are already in this directory.",
      };
    }

    for (index = 0; index < selectedEntries.length; index++) {
      if (selectedEntries[index].kind !== "directory") {
        continue;
      }
      if (candidatePath === selectedEntries[index].path || candidatePath.indexOf(selectedEntries[index].path + "/") === 0) {
        return {
          allowed: false,
          note: "A directory cannot be moved into itself or one of its descendants.",
        };
      }
    }

    return {
      allowed: true,
      note: "",
    };
  }

  /**
   * Render the current move destination search state into the dialog, keeping
   * the floating candidate list hidden until the user types and clearing it
   * again once a destination has been chosen.
   *
   * Parameters:
   *   - state: the live move-dialog state object tracking results and the selected destination path.
   *
   * Returns:
   *   - none: the results container is rebuilt in place.
   */
  function renderMoveDestinationResults(state) {
    var resultsContainer = state.resultsContainer;
    var trimmedQuery = state.searchInput.value.trim();
    var visibleResults = state.results.filter(function (directory) {
      return evaluateMoveDestination(state.selectedEntries, directory.path, state.currentPath).allowed;
    });
    var hasSelectedDestination = Boolean(state.selectedDestinationPath);

    resultsContainer.innerHTML = "";
    resultsContainer.hidden = trimmedQuery === "" || hasSelectedDestination;
    state.confirmButton.disabled = !hasSelectedDestination;

    if (!trimmedQuery || hasSelectedDestination) {
      return;
    }

    if (state.loading) {
      var loading = document.createElement("p");
      loading.className = "jotes-directory-loading";
      loading.textContent = "Searching directories...";
      resultsContainer.appendChild(loading);
      return;
    }

    if (!visibleResults.length) {
      var emptyState = document.createElement("p");
      emptyState.className = "jotes-directory-results-empty";
      emptyState.textContent = "No valid destination directories match this search.";
      resultsContainer.appendChild(emptyState);
      return;
    }

    visibleResults.forEach(function (directory) {
      var resultButton = document.createElement("button");
      var pathText = document.createElement("span");

      resultButton.type = "button";
      resultButton.className = "jotes-directory-result";

      pathText.className = "jotes-directory-result-path";
      pathText.textContent = directory.path;

      resultButton.appendChild(pathText);
      resultButton.addEventListener("click", function () {
        state.selectedDestinationPath = directory.path;
        state.results = [];
        state.loading = false;
        state.searchInput.value = "";
        renderSelectedMoveDestination(state);
        renderMoveDestinationResults(state);
      });

      resultsContainer.appendChild(resultButton);
    });
  }

  /**
   * Render or hide the embedded destination preview and its standalone arrow
   * after the user selects one move target from the floating search results.
   *
   * Parameters:
   *   - state: the live move-dialog state object tracking the selected destination preview nodes.
   *
   * Returns:
   *   - none: the arrow and embedded destination preview are shown or hidden in place.
   */
  function renderSelectedMoveDestination(state) {
    var hasSelectedDestination = Boolean(state.selectedDestinationPath);

    state.destinationArrow.hidden = !hasSelectedDestination;
    state.destinationPreview.hidden = !hasSelectedDestination;
    if (!hasSelectedDestination) {
      state.destinationPath.textContent = "";
      return;
    }

    state.destinationPath.textContent = state.selectedDestinationPath;
  }

  /**
   * Run the current directory search query for the move dialog, canceling stale
   * network requests, showing an idle state for blank input, and updating the
   * dialog state when the newest response arrives.
   *
   * Parameters:
   *   - state: the live move-dialog state object tracking query text, results, and the active AbortController.
   *
   * Returns:
   *   - none: state and UI are updated asynchronously when the request resolves.
   */
  function refreshMoveDestinationSearch(state) {
    var trimmedQuery = state.searchInput.value.trim();

    if (state.abortController) {
      state.abortController.abort();
      state.abortController = null;
    }

    setDirectoryDialogMessage(state.messageNode, "");

    if (!trimmedQuery) {
      state.loading = false;
      state.results = [];
      state.selectedDestinationPath = "";
      renderMoveDestinationResults(state);
      return;
    }

    state.abortController = new window.AbortController();
    state.loading = true;
    state.results = [];
    renderMoveDestinationResults(state);

    fetchMoveDestinationDirectories(trimmedQuery, state.currentPath, state.abortController.signal).then(function (payload) {
      state.loading = false;
      state.results = Array.isArray(payload.directories) ? payload.directories : [];
      if (payload.warning) {
        setDirectoryDialogMessage(state.messageNode, payload.warning);
      }
      renderMoveDestinationResults(state);
    }).catch(function (error) {
      if (error && error.name === "AbortError") {
        return;
      }
      state.loading = false;
      state.results = [];
      setDirectoryDialogMessage(state.messageNode, error && error.message ? error.message : "Directory search failed.");
      renderMoveDestinationResults(state);
    });
  }

  /**
   * Show the move dialog, let the user search for a valid destination directory,
   * and resolve with the chosen target path or null when cancelled.
   *
   * Parameters:
   *   - selectedEntries: the selected directory entries that the user intends to move.
   *   - currentPath: the directory path currently being viewed.
   *
   * Returns:
   *   - Promise<string|null>: the chosen destination directory path, or null when cancelled.
   */
  function promptForMoveDestination(selectedEntries, currentPath) {
    var dialogController = createDirectoryDialog({
      title: "Move selected items",
      description: "Choose a destination directory for the selected paths.",
      wide: true,
    });
    var summaryCard = document.createElement("div");
    var summaryList = document.createElement("ul");
    var field = document.createElement("div");
    var label = document.createElement("label");
    var input = document.createElement("input");
    var messageNode = document.createElement("p");
    var resultsContainer = document.createElement("div");
    var destinationPreview = document.createElement("div");
    var destinationArrow = document.createElement("div");
    var destinationCard = document.createElement("div");
    var destinationPath = document.createElement("p");
    var cancelAction = document.createElement("button");
    var confirmAction = document.createElement("button");
    var state = {
      selectedEntries: selectedEntries,
      currentPath: currentPath,
      searchInput: input,
      resultsContainer: resultsContainer,
      messageNode: messageNode,
      results: [],
      loading: false,
      selectedDestinationPath: "",
      destinationArrow: destinationArrow,
      destinationPreview: destinationPreview,
      destinationPath: destinationPath,
      confirmButton: confirmAction,
      abortController: null,
      debounceTimer: 0,
    };

    summaryCard.className = "jotes-directory-selection-summary";
    summaryList.className = "jotes-directory-selection-list";
    selectedEntries.forEach(function (entry) {
      var item = document.createElement("li");
      item.className = "jotes-directory-selection-item";
      item.textContent = entry.path;
      summaryList.appendChild(item);
    });
    summaryCard.appendChild(summaryList);

    field.className = "jotes-directory-field jotes-directory-field--search";
    label.className = "jotes-directory-label";
    label.setAttribute("for", "jotes-move-search-input");
    label.textContent = "Destination directory";
    input.className = "jotes-directory-input";
    input.id = "jotes-move-search-input";
    input.type = "search";
    input.placeholder = "Type to search by folder name or path";
    input.autocomplete = "off";
    messageNode.className = "jotes-directory-result-note";
    messageNode.hidden = true;
    resultsContainer.className = "jotes-directory-results";
    resultsContainer.hidden = true;

    destinationPreview.className = "jotes-directory-selection-summary jotes-directory-destination-preview";
    destinationPreview.hidden = true;
    destinationArrow.className = "jotes-directory-move-arrow";
    destinationArrow.hidden = true;
    destinationArrow.setAttribute("aria-hidden", "true");
    destinationArrow.textContent = "↓";
    destinationCard.className = "jotes-directory-selection-item jotes-directory-destination-card";
    destinationPath.className = "jotes-directory-result-path jotes-directory-destination-path";

    destinationCard.appendChild(destinationPath);
    destinationPreview.appendChild(destinationCard);

    cancelAction.className = "btn btn-danger";
    cancelAction.type = "button";
    cancelAction.textContent = "Cancel";
    confirmAction.className = "btn btn-success";
    confirmAction.type = "button";
    confirmAction.textContent = "Move here";
    confirmAction.disabled = true;

    field.appendChild(label);
    field.appendChild(input);
    field.appendChild(messageNode);
    field.appendChild(resultsContainer);
    dialogController.body.appendChild(summaryCard);
    dialogController.body.appendChild(destinationArrow);
    dialogController.body.appendChild(destinationPreview);
    dialogController.body.appendChild(field);
    dialogController.actions.appendChild(confirmAction);
    dialogController.actions.appendChild(cancelAction);

    cancelAction.addEventListener("click", function () {
      if (state.abortController) {
        state.abortController.abort();
      }
      window.clearTimeout(state.debounceTimer);
      dialogController.close(null);
    });
    confirmAction.addEventListener("click", function () {
      if (!state.selectedDestinationPath) {
        return;
      }
      if (state.abortController) {
        state.abortController.abort();
      }
      window.clearTimeout(state.debounceTimer);
      dialogController.close(state.selectedDestinationPath);
    });
    input.addEventListener("input", function () {
      confirmAction.disabled = true;
      state.selectedDestinationPath = "";
      renderSelectedMoveDestination(state);
      window.clearTimeout(state.debounceTimer);
      state.debounceTimer = window.setTimeout(function () {
        refreshMoveDestinationSearch(state);
      }, 180);
    });

    window.setTimeout(function () {
      input.focus();
      renderSelectedMoveDestination(state);
      renderMoveDestinationResults(state);
    }, 0);

    return dialogController.promise.then(function (result) {
      if (state.abortController) {
        state.abortController.abort();
      }
      window.clearTimeout(state.debounceTimer);
      return result;
    });
  }

  /**
   * Show a rename dialog for one selected entry and resolve with the requested
   * new basename or null when the user cancels.
   *
   * Parameters:
   *   - selectedEntry: the single selected directory entry that should be renamed.
   *
   * Returns:
   *   - Promise<string|null>: the trimmed replacement basename, or null when the dialog is cancelled.
   */
  function promptForRenameTarget(selectedEntry) {
    var dialogController = createDirectoryDialog({
      title: "Rename selected item",
      description: "Enter a new name for the selected file or directory.",
    });
    var field = document.createElement("div");
    var label = document.createElement("label");
    var input = document.createElement("input");
    var hint = document.createElement("p");
    var errorMessage = document.createElement("p");
    var cancelAction = document.createElement("button");
    var confirmAction = document.createElement("button");

    /**
     * Validate and submit the currently typed rename target for the dialog.
     *
     * Parameters:
     *   - none: the function reads the live input value and closes the dialog when it is usable.
     *
     * Returns:
     *   - none: the dialog resolves with the trimmed basename when validation passes.
     */
    function submitRenameTarget() {
      var nextName = input.value.trim();

      if (!nextName) {
        setDirectoryDialogMessage(errorMessage, "Enter a new name before renaming this item.");
        input.focus();
        return;
      }

      if (nextName === selectedEntry.name) {
        setDirectoryDialogMessage(errorMessage, "Choose a different name to rename this item.");
        input.focus();
        input.select();
        return;
      }

      dialogController.close(nextName);
    }

    field.className = "jotes-directory-field";
    label.className = "jotes-directory-label";
    label.setAttribute("for", "jotes-rename-entry-name");
    label.textContent = "New name";
    input.className = "jotes-directory-input";
    input.id = "jotes-rename-entry-name";
    input.type = "text";
    input.placeholder = selectedEntry && selectedEntry.name ? selectedEntry.name : "renamed-item";
    input.autocomplete = "off";
    input.value = selectedEntry && selectedEntry.name ? selectedEntry.name : "";
    hint.className = "jotes-directory-result-relative";
    hint.textContent = "Renaming keeps the item in the current folder. Keep the existing extension if you want the same note format.";
    errorMessage.className = "jotes-directory-result-note";
    errorMessage.hidden = true;

    cancelAction.className = "btn btn-danger";
    cancelAction.type = "button";
    cancelAction.textContent = "Cancel";
    confirmAction.className = "btn btn-primary";
    confirmAction.type = "button";
    confirmAction.textContent = "Rename";

    field.appendChild(label);
    field.appendChild(input);
    field.appendChild(hint);
    field.appendChild(errorMessage);
    dialogController.body.appendChild(field);
    dialogController.actions.appendChild(confirmAction);
    dialogController.actions.appendChild(cancelAction);

    cancelAction.addEventListener("click", function () {
      dialogController.close(null);
    });
    confirmAction.addEventListener("click", submitRenameTarget);
    input.addEventListener("keydown", function (event) {
      if (event.key === "Enter") {
        event.preventDefault();
        submitRenameTarget();
      }
    });

    window.setTimeout(function () {
      input.focus();
      input.select();
    }, 0);

    return dialogController.promise;
  }

  /**
   * Ask the user to confirm deleting the current selection with the shared
   * site-wide themed confirmation dialog.
   *
   * Parameters:
   *   - selectedEntries: the selected directory entries that would be deleted.
   *
   * Returns:
   *   - Promise<boolean>: resolves true when the user confirms the deletion.
   */
  function confirmDirectoryDelete(selectedEntries) {
    var itemSummary = summarizeSelectedEntryNames(selectedEntries);

    if (window.jotesConfirmAction) {
      return window.jotesConfirmAction({
        title: "Delete selected items?",
        message: "Delete " + formatCountLabel(selectedEntries.length, "item", "items") + ": " + itemSummary + ". This permanently removes their contents.",
        confirmLabel: selectedEntries.length === 1 ? "Delete item" : "Delete items",
        cancelLabel: "Cancel",
        kind: "danger",
      });
    }

    return Promise.resolve(window.confirm("Delete " + formatCountLabel(selectedEntries.length, "item", "items") + "?"));
  }

  /**
   * Ask the user to confirm the final move after a destination has already been
   * selected in the move dialog.
   *
   * Parameters:
   *   - selectedEntries: the selected directory entries that would be moved.
   *   - destinationPath: the destination directory path chosen by the user.
   *
   * Returns:
   *   - Promise<boolean>: resolves true when the user confirms the move.
   */
  function confirmDirectoryMove(selectedEntries, destinationPath) {
    var itemSummary = summarizeSelectedEntryNames(selectedEntries);

    if (window.jotesConfirmAction) {
      return window.jotesConfirmAction({
        title: "Move selected items?",
        message: "Move " + formatCountLabel(selectedEntries.length, "item", "items") + ": " + itemSummary + " to " + destinationPath + "? Existing filenames will be kept.",
        confirmLabel: selectedEntries.length === 1 ? "Move item" : "Move items",
        cancelLabel: "Cancel",
        kind: "success",
      });
    }

    return Promise.resolve(window.confirm("Move to " + destinationPath + "?"));
  }

  /**
   * Perform the bulk delete request for the current selection and refresh the
   * page when at least one entry was successfully removed.
   *
   * Parameters:
   *   - selectedEntries: the selected directory entries that should be deleted.
   *
   * Returns:
   *   - Promise<void>: resolves after the server responds and any follow-up reload has been scheduled.
   */
  function deleteSelectedDirectoryEntries(selectedEntries) {
    return sendDirectoryJSONRequest("/jotes/api/files/delete", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Accept": "application/json",
      },
      body: JSON.stringify({
        paths: selectedEntries.map(function (entry) {
          return entry.path;
        }),
      }),
    }).then(function (payload) {
      var deletedCount = Array.isArray(payload.deleted) ? payload.deleted.length : 0;
      var failedCount = Array.isArray(payload.failed) ? payload.failed.length : 0;

      if (deletedCount > 0 && failedCount === 0) {
        showDirectoryToast("success", "Deleted " + formatCountLabel(deletedCount, "item", "items") + ".");
        reloadDirectoryPage(300);
        return;
      }

      if (deletedCount > 0) {
        showDirectoryToast("error", "Deleted " + formatCountLabel(deletedCount, "item", "items") + ", but " + formatCountLabel(failedCount, "item", "items") + " failed. " + payload.failed[0]);
        reloadDirectoryPage(1500);
        return;
      }

      throw new Error(payload.failed && payload.failed.length ? payload.failed[0] : "Nothing was deleted.");
    });
  }

  /**
   * Perform the bulk move request for the current selection and chosen
   * destination, then refresh the directory listing when any move succeeds.
   *
   * Parameters:
   *   - selectedEntries: the selected directory entries that should be moved.
   *   - destinationPath: the destination directory path that was confirmed by the user.
   *
   * Returns:
   *   - Promise<void>: resolves after the server responds and any follow-up reload has been scheduled.
   */
  function moveSelectedDirectoryEntries(selectedEntries, destinationPath) {
    return sendDirectoryJSONRequest("/jotes/api/files/move", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Accept": "application/json",
      },
      body: JSON.stringify({
        sources: selectedEntries.map(function (entry) {
          return entry.path;
        }),
        destination: destinationPath,
      }),
    }).then(function (payload) {
      var movedCount = Array.isArray(payload.moved) ? payload.moved.length : 0;
      var failedCount = Array.isArray(payload.failed) ? payload.failed.length : 0;

      if (movedCount > 0 && failedCount === 0) {
        showDirectoryToast("success", "Moved " + formatCountLabel(movedCount, "item", "items") + " to " + destinationPath + ".");
        reloadDirectoryPage(300);
        return;
      }

      if (movedCount > 0) {
        showDirectoryToast("error", "Moved " + formatCountLabel(movedCount, "item", "items") + ", but " + formatCountLabel(failedCount, "item", "items") + " failed. " + payload.failed[0]);
        reloadDirectoryPage(1500);
        return;
      }

      throw new Error(payload.failed && payload.failed.length ? payload.failed[0] : "Nothing was moved.");
    });
  }

  /**
   * Perform a single-entry rename through the shared move endpoint so the
   * selected item keeps its current parent directory but receives a new name.
   *
   * Parameters:
   *   - selectedEntry: the single selected directory entry that should be renamed.
   *   - targetName: the validated basename that should replace the current one.
   *
   * Returns:
   *   - Promise<void>: resolves after the server responds and any follow-up reload has been scheduled.
   */
  function renameSelectedDirectoryEntry(selectedEntry, targetName) {
    var currentPath = directoryView.getAttribute("data-current-path") || "/";

    return sendDirectoryJSONRequest("/jotes/api/files/move", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Accept": "application/json",
      },
      body: JSON.stringify({
        sources: [selectedEntry.path],
        destination: currentPath,
        targetName: targetName,
      }),
    }).then(function (payload) {
      var movedCount = Array.isArray(payload.moved) ? payload.moved.length : 0;
      var failedCount = Array.isArray(payload.failed) ? payload.failed.length : 0;

      if (movedCount > 0 && failedCount === 0) {
        showDirectoryToast("success", "Renamed " + selectedEntry.name + " to " + targetName + ".");
        reloadDirectoryPage(300);
        return;
      }

      if (movedCount > 0) {
        showDirectoryToast("error", "Renamed " + selectedEntry.name + ", but a follow-up refresh is needed. " + payload.failed[0]);
        reloadDirectoryPage(1500);
        return;
      }

      throw new Error(payload.failed && payload.failed.length ? payload.failed[0] : "Nothing was renamed.");
    });
  }

  /**
   * Handle the Create button by first asking which create or upload flow the
   * user wants, then launching the matching dialog and backend request for
   * that choice.
   *
   * Parameters:
   *   - none: the function reads the current directory path from the page data attributes.
   *
   * Returns:
   *   - none: async work is kicked off immediately and user feedback is shown as needed.
   */
  function handleDirectoryCreateClick() {
    promptForCreateOption().then(function (createMode) {
      if (!createMode) {
        return null;
      }

      if (createMode === "note") {
        return promptForNewNoteEntry().then(function (createRequest) {
          if (!createRequest) {
            return;
          }
          return createDirectoryEntry(createRequest);
        });
      }

      if (createMode === "directory") {
        return promptForNewDirectoryName().then(function (createRequest) {
          if (!createRequest) {
            return;
          }
          return createDirectoryEntry(createRequest);
        });
      }

      if (createMode === "upload") {
        return promptForUploadFile().then(function (selectedFile) {
          if (!selectedFile) {
            return;
          }
          return uploadDirectoryFile(selectedFile);
        });
      }

      return null;
    }).catch(function (error) {
      var fallbackMessage = "The requested create action could not be completed.";
      if (error && error.directoryFeedbackShown) {
        return;
      }
      showDirectoryToast("error", error && error.message ? error.message : fallbackMessage);
    });
  }

  /**
   * Handle the Rename button by collecting a single selected entry, prompting
   * for its replacement basename, and then submitting the rename request.
   *
   * Parameters:
   *   - none: the function reads the current row selection from the directory table.
   *
   * Returns:
   *   - none: async work is kicked off immediately and user feedback is shown as needed.
   */
  function handleDirectoryRenameClick() {
    var selectedEntries = getSelectedDirectoryEntries();
    var selectedEntry = null;

    if (selectedEntries.length !== 1) {
      return;
    }
    if (selectionContainsJotesCompanionEntries(selectedEntries)) {
      showDirectoryToast("error", "Managed .jotes companion folders move with their note and cannot be renamed directly.");
      return;
    }

    selectedEntry = selectedEntries[0];
    promptForRenameTarget(selectedEntry).then(function (targetName) {
      if (!targetName) {
        return;
      }
      return renameSelectedDirectoryEntry(selectedEntry, targetName).catch(function (error) {
        showDirectoryToast("error", error && error.message ? error.message : "The selected item could not be renamed.");
      });
    });
  }

  /**
   * Handle the Delete button by confirming the current selection and then
   * sending the bulk-delete request to the server.
   *
   * Parameters:
   *   - none: the function reads the current row selection from the directory table.
   *
   * Returns:
   *   - none: async work is kicked off immediately and user feedback is shown as needed.
   */
  function handleDirectoryDeleteClick() {
    var selectedEntries = getSelectedDirectoryEntries();

    if (!selectedEntries.length) {
      return;
    }

    confirmDirectoryDelete(selectedEntries).then(function (confirmed) {
      if (!confirmed) {
        return;
      }
      return deleteSelectedDirectoryEntries(selectedEntries).catch(function (error) {
        showDirectoryToast("error", error && error.message ? error.message : "The selected items could not be deleted.");
      });
    });
  }

  /**
   * Handle the Move button by showing the destination search dialog, confirming
   * the final choice, and then submitting the bulk move request.
   *
   * Parameters:
   *   - none: the function reads the current row selection and page path from the document.
   *
   * Returns:
   *   - none: async work is kicked off immediately and user feedback is shown as needed.
   */
  function handleDirectoryMoveClick() {
    var selectedEntries = getSelectedDirectoryEntries();
    var currentPath = directoryView.getAttribute("data-current-path") || "/";

    if (!selectedEntries.length) {
      return;
    }
    if (selectionContainsJotesCompanionEntries(selectedEntries)) {
      showDirectoryToast("error", "Managed .jotes companion folders move with their note and cannot be moved directly.");
      return;
    }

    promptForMoveDestination(selectedEntries, currentPath).then(function (destinationPath) {
      if (!destinationPath) {
        return;
      }
      return confirmDirectoryMove(selectedEntries, destinationPath).then(function (confirmed) {
        if (!confirmed) {
          return;
        }
        return moveSelectedDirectoryEntries(selectedEntries, destinationPath).catch(function (error) {
          showDirectoryToast("error", error && error.message ? error.message : "The selected items could not be moved.");
        });
      });
    });
  }

  /**
   * Toggle every currently visible row selection when the header checkbox is
   * checked or unchecked in edit mode.
   *
   * Parameters:
   *   - checked: true to select all visible rows, or false to clear them.
   *
   * Returns:
   *   - none: visible row checkboxes are updated in place.
   */
  function setVisibleDirectorySelections(checked) {
    getDirectoryEntryRows().forEach(function (row) {
      var checkbox = getDirectoryRowCheckbox(row);
      if (!checkbox || !isDirectoryRowVisible(row)) {
        return;
      }
      checkbox.checked = Boolean(checked);
    });

    syncDirectoryActionControls();
  }

  /**
   * Intercept row clicks in edit mode so tapping a row toggles its checkbox
   * instead of following the directory row navigation behavior from main.js.
   *
   * Parameters:
   *   - event: the captured click event from the directory table.
   *
   * Returns:
   *   - none: the relevant row checkbox is toggled and navigation is suppressed when edit mode is active.
   */
  function handleDirectoryTableClickCapture(event) {
    var row = null;
    var checkbox = null;

    if (!directoryState.editMode) {
      return;
    }

    if (event.target.closest(".btn")) {
      return;
    }

    row = event.target.closest("tr[data-entry-path]");
    if (!row) {
      return;
    }

    if (event.target.closest("input[type='checkbox']")) {
      event.stopPropagation();
      return;
    }

    event.preventDefault();
    event.stopPropagation();

    checkbox = getDirectoryRowCheckbox(row);
    if (!checkbox) {
      return;
    }

    checkbox.checked = !checkbox.checked;
    syncDirectoryActionControls();
  }

  /**
   * Re-run edit-mode selection cleanup after any directory filter toggle
   * changes the set of visible rows, including the dedicated .jotes companion
   * visibility checkbox.
   *
   * Parameters:
   *   - none: the function reads the existing row visibility set after the current event turn completes.
   *
   * Returns:
   *   - none: hidden selections are cleared and bulk controls are refreshed.
   */
  function handleDirectoryFilterVisibilityChange() {
    clearSelectionsForHiddenRows();
    syncDirectoryActionControls();
  }

  /**
   * Wire the directory action buttons, selection controls, and edit-mode row
   * click interception for the current directory page.
   *
   * Parameters:
   *   - none: the function binds listeners to the current page's directory controls.
   *
   * Returns:
   *   - none: all directory action behavior becomes live for this page instance.
   */
  function initializeDirectoryActions() {
    if (createButton) {
      createButton.addEventListener("click", handleDirectoryCreateClick);
    }
    if (editButton) {
      editButton.addEventListener("click", function () {
        setDirectoryEditMode(true);
      });
    }
    if (cancelButton) {
      cancelButton.addEventListener("click", function () {
        setDirectoryEditMode(false);
      });
    }
    if (renameButton) {
      renameButton.addEventListener("click", handleDirectoryRenameClick);
    }
    if (deleteButton) {
      deleteButton.addEventListener("click", handleDirectoryDeleteClick);
    }
    if (moveButton) {
      moveButton.addEventListener("click", handleDirectoryMoveClick);
    }
    if (selectAllCheckbox) {
      selectAllCheckbox.addEventListener("change", function () {
        setVisibleDirectorySelections(selectAllCheckbox.checked);
      });
    }
    if (directoryTable) {
      directoryTable.addEventListener("click", handleDirectoryTableClickCapture, true);
      directoryTable.addEventListener("change", function (event) {
        if (event.target && event.target.classList && event.target.classList.contains("dir-select-checkbox")) {
          syncDirectoryActionControls();
        }
      });
    }
    if (auxiliaryVisibilityCheckbox) {
      auxiliaryVisibilityCheckbox.addEventListener("change", handleDirectoryFilterVisibilityChange);
    }
    if (hiddenVisibilityCheckbox) {
      hiddenVisibilityCheckbox.addEventListener("change", handleDirectoryFilterVisibilityChange);
    }
    if (jotesVisibilityCheckbox) {
      jotesVisibilityCheckbox.addEventListener("change", handleDirectoryFilterVisibilityChange);
    }

    syncDirectoryActionControls();
  }

  initializeDirectoryActions();
})();
