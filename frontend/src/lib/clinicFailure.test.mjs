import assert from 'node:assert/strict'
import test from 'node:test'
import { describeClinicFailure, isClinicAuthFailure } from './clinicFailure.ts'

test('file permission failures do not replace the workspace with an auth screen', () => {
  assert.equal(isClinicAuthFailure(403, 'access denied'), false)
  assert.equal(isClinicAuthFailure(403, 'invalid project file'), false)
  assert.equal(isClinicAuthFailure(401, 'invalid token'), true)
  assert.equal(isClinicAuthFailure(403, 'denied', 'account_not_allowed'), true)
  assert.equal(isClinicAuthFailure(403, 'origin not allowed'), true)
  assert.equal(isClinicAuthFailure(403, 'invalid host'), true)
  assert.doesNotMatch(describeClinicFailure({ status: 403, kind: 'http', message: 'access denied' }).guidance, /ALLOWED_ORIGINS/)
})

test('legacy invalid token response offers session renewal without accusing the account', () => {
  const view = describeClinicFailure({status:401,kind:'http',message:'invalid token'})
  assert.equal(view.sessionError, true)
  assert.equal(view.retryLabel, '로그인 정보 갱신')
  assert.doesNotMatch(view.title + view.guidance, /허용|ALLOWED/)
})

test('only an explicit identity rejection tells the user to change accounts', () => {
  const denied = describeClinicFailure({status:403,kind:'http',code:'account_not_allowed',message:'denied'})
  assert.equal(denied.sessionError, false)
  assert.match(denied.guidance, /계정으로 전환/)
  const origin = describeClinicFailure({status:403,kind:'auth',message:'origin not allowed'})
  assert.equal(origin.sessionError, false)
  assert.match(origin.guidance, /ALLOWED_ORIGINS/)
  assert.doesNotMatch(origin.guidance, /계정으로 전환/)
})

test('an offline clinic offers reconnection without refreshing the login', () => {
  const view = describeClinicFailure({status:null,kind:'connection',message:'offline'})
  assert.equal(view.sessionError, false)
  assert.equal(view.retryLabel, '다시 연결')
  assert.match(view.guidance, /clinic을 실행/)
})
