"""MkDocs hooks for the litmus-lite site."""

import os
import subprocess

from mkdocs.exceptions import PluginError


def on_files(files, config):
    """Drop every file under docs/ that git ignores.

    Fails the build when git cannot answer, rather than risk publishing an
    ignored file.
    """
    root = os.path.dirname(config["config_file_path"])
    try:
        out = subprocess.run(
            ["git", "ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--", config["docs_dir"]],
            cwd=root, capture_output=True, text=True, check=True,
        ).stdout
    except (OSError, subprocess.CalledProcessError) as err:
        raise PluginError(f"cannot list the files git ignores: {err}")
    ignored = {os.path.realpath(os.path.join(root, p)) for p in out.split("\0") if p}
    for f in list(files):
        if f.abs_src_path and os.path.realpath(f.abs_src_path) in ignored:
            files.remove(f)
    return files
