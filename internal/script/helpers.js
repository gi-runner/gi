(function(k, native) {
    k.keysOf = function(x) { return Object.keys(x); };
    k.valuesOf = function(x) {
        return Object.keys(x).map(function(key) { return x[key]; });
    };
    k.sizeOf = function(x) {
        if (x == null) return 0;
        return typeof x === 'object' && !Array.isArray(x) ? Object.keys(x).length : x.length;
    };
    k.typeOf = function(x) {
        if (x === null) return 'null';
        if (x && x.__xml === true) return 'xml';
        if (Array.isArray(x)) return 'list';
        return typeof x === 'object' ? 'map' : typeof x;
    };
    k.forEach = function(x, f) {
        if (Array.isArray(x)) {
            x.forEach(f);
        } else {
            Object.keys(x).forEach(function(key, i) { f(key, x[key], i); });
        }
    };
    k.map = function(x, f) { return x.map(f); };
    k.filter = function(x, f) { return x.filter(f); };
    k.repeat = function(n, f) {
        if (!Number.isInteger(n) || n < 0) throw Error('repeat requires a nonnegative integer');
        var out = [];
        for (var i = 0; i < n; i++) out.push(f(i));
        return out;
    };
    k.range = function(a, b, step) {
        step = step === undefined ? (a <= b ? 1 : -1) : step;
        if (!Number.isFinite(a) || !Number.isFinite(b) || !Number.isFinite(step) ||
            step === 0 || (b - a) * step < 0) throw Error('invalid range');
        var out = [];
        for (var i = a; step > 0 ? i <= b : i >= b; i += step) {
            out.push(i);
            if (i + step === i) throw Error('range does not advance');
        }
        return out;
    };
    function equal(a, b) {
        if (Object.is(a, b) || a === b) return true;
        if (a === null || b === null || typeof a !== 'object' || typeof b !== 'object') return false;
        if (Array.isArray(a) !== Array.isArray(b)) return false;
        var keys = Object.keys(a);
        return keys.length === Object.keys(b).length && keys.every(function(key) {
            return Object.prototype.hasOwnProperty.call(b, key) && equal(a[key], b[key]);
        });
    }
    k.distinct = function(x) {
        if (!Array.isArray(x)) return [];
        return x.filter(function(v, i) {
            return x.findIndex(function(w) { return equal(v, w); }) === i;
        });
    };
    k.sort = function(x, f) {
        return x.map(function(v, i) { return {value: v, key: f ? f(v, i) : v, index: i}; })
            .sort(function(a, b) {
                var left = a.key, right = b.key;
                if (left == null || right == null) {
                    return left == right ? a.index - b.index : left == null ? -1 : 1;
                }
                if (typeof left !== typeof right || typeof left === 'object') {
                    left = String(left);
                    right = String(right);
                }
                return left < right ? -1 : left > right ? 1 : a.index - b.index;
            }).map(function(pair) { return pair.value; });
    };
    k.filterKeys = function(x, keys) {
        if (!Array.isArray(keys)) {
            keys = keys !== null && typeof keys === 'object' ? Object.keys(keys) : Array.prototype.slice.call(arguments, 1);
        }
        var out = {};
        keys.forEach(function(key) {
            if (Object.prototype.hasOwnProperty.call(x, key)) out[key] = x[key];
        });
        return out;
    };
    k.mapWithKey = function(x, key) {
        return x.map(function(v) { var out = {}; out[key] = v; return out; });
    };
    // Merge is shallow: a later object replaces an earlier nested value.
    k.merge = function() {
        return Object.assign.apply(Object, [{}].concat(Array.prototype.slice.call(arguments)));
    };
    k.append = function() { return [].concat.apply([], arguments); };
    k.appendTo = function(list) {
        var items = [].concat.apply([], Array.prototype.slice.call(arguments, 1));
        items.forEach(function(item) { list.push(item); });
        return list;
    };
    k.lowerCase = function(x) {
        return typeof x === 'string' ? x.toLowerCase() : JSON.parse(JSON.stringify(x).toLowerCase());
    };
    k.pretty = function(x) { return x && x.__xml === true ? x.toString() : JSON.stringify(x, null, 2); };
    k.toString = function(x) {
        var text = native.text(x);
        if (text !== null) return text;
        return x && x.__xml === true ? x.toString() : JSON.stringify(x);
    };
    k.fail = function(message) {
        throw Error(message === undefined ? 'explicit failure' : String(message));
    };
    k.toCsv = function(list) {
        if (list == null || (Array.isArray(list) && list.length === 0)) return '';
        if (!Array.isArray(list)) throw Error('toCsv expects a list of objects');
        function cell(x) {
            var s = x == null ? '' : typeof x === 'object' ? JSON.stringify(x) : String(x);
            return /[",\r\n]/.test(s) ? '"' + s.replace(/"/g, '""') + '"' : s;
        }
        var keys = Object.keys(list[0]);
        var rows = [keys.map(cell).join(',')];
        list.forEach(function(row) {
            rows.push(Object.keys(row).map(function(key) { return cell(row[key]); }).join(','));
        });
        return rows.join('\r\n') + '\r\n';
    };
    k.expect = function(subject) { return expectation(subject, false); };

    // Chai-style assertions: property getters such as .true assert on read.
    function expectation(subject, negated) {
        function check(pass, message) {
            if (pass === negated) throw Error(negated ? 'expected condition to be false' : message);
        }
        function next(s) { return expectation(arguments.length ? s : subject, false); }
        function by(op) {
            return function(expected) {
                var r = native.match(subject, expected, op);
                check(r.pass, r.message);
                return next();
            };
        }
        function plain(x) { return x !== null && typeof x === 'object' && !Array.isArray(x); }
        function keys(op) {
            return function(expected) {
                if (plain(subject)) {
                    var r = native.match(Object.keys(subject), expected, op);
                    check(r.pass, r.message);
                } else {
                    check(false, 'not an object');
                }
                return next();
            };
        }
        function numeric(word, test) {
            return function(rhs) {
                check(typeof subject === 'number' && typeof rhs === 'number' && test(subject, rhs),
                    subject + ' is not ' + word + ': ' + rhs);
                return next();
            };
        }
        function state(word, pass, shown) {
            return function() { check(pass(), 'actual: ' + subject + ', expected: ' + (shown || word)); return next(); };
        }
        function api(methods, getters) {
            var o = Object.assign({}, methods);
            Object.keys(getters).forEach(function(name) {
                Object.defineProperty(o, name, {get: getters[name]});
            });
            return o;
        }
        function typeName(x) { return x === null ? 'null' : Array.isArray(x) ? 'array' : typeof x; }
        function typed(expected) {
            check(typeName(subject) === String(expected), 'actual: ' + typeName(subject) + ', expected: ' + expected);
            return next();
        }
        function walk(x, path) {
            var parts = String(path).match(/[^.[\]]+/g) || [];
            for (var i = 0; i < parts.length; i++) {
                if (x === null || typeof x !== 'object' || !Object.prototype.hasOwnProperty.call(x, parts[i])) return {found: false};
                x = x[parts[i]];
            }
            return {found: true, value: x};
        }
        function empty() {
            if (typeof subject === 'string') return subject.length === 0;
            if (Array.isArray(subject)) return subject.length === 0;
            return plain(subject) && Object.keys(subject).length === 0;
        }
        var be, to, have;
        be = api({
            a: typed, an: typed,
            oneOf: function(list) {
                var r = native.match(list, subject, 'contains');
                check(r.pass, r.message);
                return next();
            },
            above: numeric('above', function(a, b) { return a > b; }),
            below: numeric('below', function(a, b) { return a < b; }),
            within: function(lo, hi) {
                check(subject >= lo && subject <= hi, subject + ' not within ' + lo + ', ' + hi);
                return next();
            },
            closeTo: function(expected, delta) {
                check(Math.abs(subject - expected) <= delta, subject + ' not close to ' + expected + ' ± ' + delta);
                return next();
            }
        }, {
            not: function() { return expectation(subject, !negated).to.be; },
            that: function() { return be; }, and: function() { return be; }, which: function() { return be; },
            true: state('true', function() { return subject === true; }),
            false: state('false', function() { return subject === false; }),
            null: state('null', function() { return subject === null; }),
            undefined: state('undefined', function() { return subject == null; }),
            ok: state('ok', function() { return !!subject; }, '(truthy)'),
            empty: state('empty', empty, '(empty)'),
            at: function() {
                return {
                    least: numeric('at least', function(a, b) { return a >= b; }),
                    most: numeric('at most', function(a, b) { return a <= b; })
                };
            }
        });
        be.approximately = be.closeTo;
        ['true', 'false', 'null', 'undefined'].forEach(function(name) {
            Object.defineProperty(be, '_' + name, Object.getOwnPropertyDescriptor(be, name));
        });
        have = api({
            length: function(n) {
                if (typeof n !== 'number') { check(false, 'not a number: ' + n); return next(); }
                var size = typeof subject === 'string' || Array.isArray(subject) ? subject.length :
                    plain(subject) ? subject.length : undefined;
                check(size === n, 'actual: ' + JSON.stringify(subject) + ', expected: (length=' + n + ')');
                return next();
            },
            property: function(name, expected) {
                if (!plain(subject)) { check(false, 'not an object'); return next(); }
                var has = Object.prototype.hasOwnProperty.call(subject, name);
                if (arguments.length > 1 && has) {
                    var r = native.match(subject[name], expected, '==');
                    check(r.pass, r.message);
                } else check(has, arguments.length > 1 ? 'property ' + name + ' not found' : 'does not contain property: ' + name);
                return has && subject[name] != null ? next(subject[name]) : next();
            },
            keys: keys('contains only')
        }, {
            not: function() { return expectation(subject, !negated).to.have; },
            that: function() { return have; }, and: function() { return have; }, which: function() { return have; },
            all: function() { return {keys: keys('contains')}; },
            any: function() { return {keys: keys('contains any')}; },
            nested: function() {
                return {property: function(path, expected) {
                    var got = walk(subject, path);
                    if (arguments.length > 1 && got.found) {
                        var r = native.match(got.value, expected, '==');
                        check(r.pass, r.message);
                    } else {
                        check(got.found, 'expected to have property: ' + path);
                    }
                    return next();
                }};
            }
        });
        to = api({
            equal: by('=='), eql: by('=='),
            include: by('contains'), contain: by('contains'),
            match: function(re) {
                if (typeof subject !== 'string') check(false, 'not a string');
                else if (!(re instanceof RegExp)) check(false, 'not a regex: ' + re);
                else check(re.test(subject), 'actual: ' + subject + ', expected: ' + re);
                return next();
            }
        }, {
            not: function() { return expectation(subject, !negated).to; },
            that: function() { return to; }, and: function() { return to; }, which: function() { return to; },
            deep: function() { return {include: by('contains deep')}; },
            be: function() { return be; },
            have: function() { return have; },
            exist: state('exist', function() { return subject != null; }, '(exists)')
        });
        var self = api({
            include: by('contains'), contain: by('contains'), equal: by('=='), eql: by('==')
        }, {
            to: function() { return to; },
            not: function() { return expectation(subject, true); },
            that: function() { return self; }, and: function() { return self; }, which: function() { return self; },
            be: function() { return be; }, is: function() { return be; },
            have: function() { return have; }, has: function() { return have; }
        });
        return self;
    }
});
