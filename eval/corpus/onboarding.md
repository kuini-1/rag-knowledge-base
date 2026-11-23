# Getting Started

This guide takes you from a new account to your first uploaded file in about ten minutes.

## 1. Create an organization

Sign up at the console and create an organization. The person who creates it becomes its Owner. Owners can invite others as Admin, Developer, or Viewer. Viewers can read data and settings but cannot change anything.

## 2. Install the CLI

The `lark` command-line tool runs on macOS, Linux, and Windows.

- macOS: `brew install larkspur/tap/lark`
- Linux: download the binary from the releases page, or run the install script.
- Windows: `winget install Larkspur.Lark`

Check the installation with `lark version`. The CLI checks for updates once a day and prints a notice when one is available; set `LARK_NO_UPDATE_CHECK=1` to turn this off.

## 3. Log in

Run `lark login`. It opens a browser window, and after you approve, the CLI stores a short-lived token in your system keychain. For CI systems, create an API key instead and set it in the `LARK_API_KEY` environment variable.

## 4. Create a bucket

Buckets hold your objects. Create one with:

`lark bucket create my-first-bucket --region eu-frankfurt-1`

If you leave out `--region`, the CLI uses the default region from your profile, which is us-virginia-1 for new accounts.

## 5. Upload a file

`lark cp ./report.pdf lark://my-first-bucket/reports/`

Files above 100 MB are uploaded in parts automatically, and an interrupted upload resumes where it stopped when you run the same command again.

## Next steps

Set up lifecycle rules to move old objects to cold storage, enable versioning on buckets that hold important data, and invite your team. Versioning keeps previous versions of an object for as long as your lifecycle rules allow.
