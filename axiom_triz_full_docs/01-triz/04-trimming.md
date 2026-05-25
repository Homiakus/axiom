# Trimming пользовательского слоя

## Цель trimming

Удалить всё, что не нужно пользователю для понимания поведения системы.

## Что удаляем с поверхности

| Удаляемое понятие | Кто берёт функцию |
|---|---|
| `computed` | `condition` + normalizer |
| `fact` | `condition` + runtime |
| `activity` | `function` + managed action wrapper |
| `policy` | `profile` + defaults |
| `claim` | `always` |
| `query` | `view` + built-in views |
| `signal` | `event` |
| explicit CRFG | normalizer/runtime |
| manual registration | codegen |
| manual trace | history engine |

## Пример trimming

### До

```axiom
computed userReady: Bool =
  User.id exists and User.email exists

fact RegisteredUser when:
  userReady

activity SendWelcomeEmail:
  require:
    RegisteredUser
  input:
    email = User.email
  output:
    sent: Bool
  effect: external
  policy: emailPolicy

rule sendWelcomeEmail:
  on changed(User.email)
  require:
    RegisteredUser
  run: SendWelcomeEmail
  write:
    User.welcomeSent = output.sent
```

### После

```axiom
condition RegisteredUser:
  User.id exists
  User.email exists

rule SendWelcomeEmail when:
  RegisteredUser
  User.welcomeSent == false
do:
  result = SendWelcomeEmail(User.email)
then:
  set User.welcomeSent = result.sent
```

## Trimming не уничтожает runtime

Важно:

```text
Мы не удаляем computed/fact/activity/policy/claim из engine.
Мы удаляем их из обязательного пользовательского мышления.
```

Внутри всё равно остаётся строгий IR.

## Главный критерий

Если сущность нужна runtime, но не нужна пользователю, она должна стать:

```text
generated
inferred
defaulted
visualized
advanced-only
```
