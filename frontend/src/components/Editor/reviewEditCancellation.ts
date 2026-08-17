import type { ReviewStatus } from '../../store'

/** First edit cancels a live review; subsequent keystrokes share that cancellation. */
export function shouldCancelReviewOnEdit(status: ReviewStatus, cancellationRequested: boolean): boolean {
  return !cancellationRequested && (status === 'requesting' || status === 'reviewing')
}
