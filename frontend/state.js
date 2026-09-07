const MAX_IMAGE_BYTES = 10 * 1024 * 1024;

// state holds the observable UI state of the page: which file is selected,
// whether a submission is in flight, and whether a job has been accepted.
export const state = {
  selectedFile: null,
  previewObjectURL: null,
  jobAccepted: false,
  isSubmitting: false,
};

export { MAX_IMAGE_BYTES };