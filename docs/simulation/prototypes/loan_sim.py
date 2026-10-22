"""Thought-experiment: loan approval decision graph.
World x Policy x Seed -> Outcome. Exact enumeration, backward induction, Monte Carlo check."""
import itertools, random, statistics

WORLDS = {
    "base":      dict(p_bad=0.20, loss=4000),
    "recession": dict(p_bad=0.35, loss=6000),
}
GAIN = 1000          # profit from approving a good applicant
SCORE_COST = 20      # AI risk score
REVIEW_COST = 150    # human review
REVIEW_DAYS = 2
DROP_GOOD = 0.30     # good applicants who leave during review wait (bad ones stay)
P_LOW = {"good": 0.90, "bad": 0.30}   # P(score=low | type)
P_PASS = {"good": 0.95, "bad": 0.10}  # P(review=pass | type)

# Policy = choices at every information set.
# D1: score or not. D2[obs1]: approve/reject/review. D3[(obs1, obs2)]: approve/reject.
OBS1 = {"skip": ["none"], "score": ["low", "high"]}

def all_policies():
    for d1 in ["skip", "score"]:
        obs = OBS1[d1]
        for d2 in itertools.product(["approve", "reject", "review"], repeat=len(obs)):
            d2m = dict(zip(obs, d2))
            review_obs = [o for o in obs if d2m[o] == "review"]
            keys = [(o, r) for o in review_obs for r in ["pass", "fail"]]
            for d3 in itertools.product(["approve", "reject"], repeat=len(keys)):
                yield {"d1": d1, "d2": d2m, "d3": dict(zip(keys, d3))}

def simulate_one(world, pol, rng):
    """One application through the graph; returns (profit, days, approved)."""
    w = WORLDS[world]
    typ = "bad" if rng.random() < w["p_bad"] else "good"
    profit, days = 0.0, 0
    if pol["d1"] == "score":
        profit -= SCORE_COST
        o1 = "low" if rng.random() < P_LOW[typ] else "high"
    else:
        o1 = "none"
    a = pol["d2"][o1]
    if a == "review":
        profit -= REVIEW_COST
        days += REVIEW_DAYS
        if typ == "good" and rng.random() < DROP_GOOD:
            return profit, days, False          # applicant left
        o2 = "pass" if rng.random() < P_PASS[typ] else "fail"
        a = pol["d3"][(o1, o2)]
    if a == "approve":
        profit += GAIN if typ == "good" else -w["loss"]
        return profit, days, True
    return profit, days, False

def exact(world, pol):
    """Exact expectation by summing over all chance outcomes."""
    w = WORLDS[world]
    ev = days = appr = 0.0
    for typ, pt in [("good", 1 - w["p_bad"]), ("bad", w["p_bad"])]:
        obs = [("none", 1.0)] if pol["d1"] == "skip" else [("low", P_LOW[typ]), ("high", 1 - P_LOW[typ])]
        for o1, po in obs:
            p = pt * po
            base = -SCORE_COST if pol["d1"] == "score" else 0
            a = pol["d2"][o1]
            branches = [(a, p, base, 0)]
            if a == "review":
                stay = 1 - DROP_GOOD if typ == "good" else 1.0
                ev += p * (1 - stay) * (base - REVIEW_COST); days += p * (1 - stay) * REVIEW_DAYS
                branches = [(pol["d3"][(o1, o2)], p * stay * pr, base - REVIEW_COST, REVIEW_DAYS)
                            for o2, pr in [("pass", P_PASS[typ]), ("fail", 1 - P_PASS[typ])]]
            for act, pb, cost, d in branches:
                pay = (GAIN if typ == "good" else -w["loss"]) if act == "approve" else 0
                ev += pb * (cost + pay); days += pb * d; appr += pb * (act == "approve")
    return ev, days, appr

def describe(pol):
    s = [f"D1={pol['d1']}"] + [f"{o}->{a}" for o, a in pol["d2"].items()]
    s += [f"{o}/{r}->{a}" for (o, r), a in pol["d3"].items()]
    return ", ".join(s)

pols = list(all_policies())
print(f"policy space: {len(pols)} deterministic policies\n")
results = {}
for world in WORLDS:
    scored = sorted(((exact(world, p), p) for p in pols), key=lambda x: -x[0][0])
    results[world] = {describe(p): r for r, p in scored}
    print(f"== world: {world} ==")
    for (ev, d, ap), p in scored[:4]:
        print(f"  EV {ev:8.1f}  days {d:.2f}  approve {ap:.1%}  | {describe(p)}")
    worst = scored[-1]
    print(f"  ... worst EV {worst[0][0]:.1f} | {describe(worst[1])}")
    # baselines
    for name in ["D1=skip, none->approve", "D1=skip, none->reject"]:
        print(f"  baseline {name}: EV {results[world][name][0]:.1f}")
    # Monte Carlo check of the best
    best = scored[0][1]
    rng = random.Random(42)
    runs = [simulate_one(world, best, rng)[0] for _ in range(200_000)]
    print(f"  MC check best: mean {statistics.fmean(runs):.1f}  (n=200k, seed 42)")
    print(f"  MC P(loss on an application) {sum(r < 0 for r in runs)/len(runs):.1%}\n")

# Cross-world: regret of applying one world's optimum in the other world
print("== cross-world regret ==")
best = {w: max(results[w].items(), key=lambda kv: kv[1][0])[0] for w in WORLDS}
for pw in WORLDS:
    for tw in WORLDS:
        print(f"  policy tuned for {pw:9s} run in {tw:9s}: EV {results[tw][best[pw]][0]:8.1f}"
              f"  (regret {results[tw][best[tw]][0]-results[tw][best[pw]][0]:.1f})")
# Robust: maximize expected EV under a 70/30 world prior
prior = {"base": 0.7, "recession": 0.3}
rob = max(results["base"], key=lambda k: sum(prior[w] * results[w][k][0] for w in WORLDS))
print(f"\nrobust (70/30 prior): {rob}")
for w in WORLDS:
    print(f"  in {w}: EV {results[w][rob][0]:.1f}")

# Value of information: best with score vs best without
for w in WORLDS:
    bs = max(v[0] for k, v in results[w].items() if k.startswith("D1=score"))
    bk = max(v[0] for k, v in results[w].items() if k.startswith("D1=skip"))
    print(f"VOI of AI score in {w}: {bs - bk:.1f} per application (cost {SCORE_COST})")
