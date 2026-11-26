// Package colab provides an authenticated Go client for Google Colab runtimes.
//
// Use NewClient with NewADCAuth for backend workloads, then create a
// RuntimeManager to allocate or manage sessions. Executor runs code over the
// runtime Jupyter protocol; ContentsClient manages remote files. The package
// is independent from the Python google-colab-cli distribution.
package colab
