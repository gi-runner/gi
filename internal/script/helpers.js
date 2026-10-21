(function(k) {
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
    k.toString = function(x) { return x && x.__xml === true ? x.toString() : typeof x === 'string' ? x : JSON.stringify(x); };
    k.fail = function(message) {
        throw Error(message === undefined ? 'explicit failure' : String(message));
    };
});
